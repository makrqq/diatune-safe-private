from __future__ import annotations

import asyncio
import hashlib
import math
import random
from datetime import UTC, datetime, timedelta

import httpx

from diatune_safe.domain import CarbEvent, GlucosePoint, InsulinEvent, PatientDataset


class DataSourceError(RuntimeError):
    pass


class NightscoutDataSource:
    def __init__(self, *, base_url: str, api_secret: str, timeout_seconds: int = 30) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_secret = api_secret.strip()
        self.timeout_seconds = timeout_seconds

    async def fetch_dataset(self, *, patient_id: str, since: datetime, until: datetime) -> PatientDataset:
        del patient_id  # Nightscout usually stores single patient context.
        headers = self._headers()
        async with httpx.AsyncClient(timeout=self.timeout_seconds) as client:
            glucose_resp, treatments_resp = await asyncio.gather(
                client.get(
                    f"{self.base_url}/api/v1/entries/sgv.json",
                    params=self._query_params(since, until),
                    headers=headers,
                ),
                client.get(
                    f"{self.base_url}/api/v1/treatments.json",
                    params=self._query_params(since, until),
                    headers=headers,
                ),
            )

        if glucose_resp.status_code >= 400:
            raise DataSourceError(f"Nightscout glucose request failed with {glucose_resp.status_code}")
        if treatments_resp.status_code >= 400:
            raise DataSourceError(f"Nightscout treatments request failed with {treatments_resp.status_code}")

        glucose_rows = glucose_resp.json()
        treatment_rows = treatments_resp.json()
        glucose = self._parse_glucose(glucose_rows)
        carbs, insulin = self._parse_treatments(treatment_rows)
        return PatientDataset(glucose=glucose, carbs=carbs, insulin=insulin)

    def _headers(self) -> dict[str, str]:
        if not self.api_secret:
            return {}
        hashed = self.api_secret.lower()
        if len(hashed) != 40 or not all(ch in "0123456789abcdef" for ch in hashed):
            hashed = hashlib.sha1(self.api_secret.encode("utf-8")).hexdigest()
        return {"API-SECRET": hashed}

    def _query_params(self, since: datetime, until: datetime) -> dict[str, str]:
        return {
            "find[created_at][$gte]": since.isoformat(),
            "find[created_at][$lte]": until.isoformat(),
            "count": "100000",
        }

    def _parse_glucose(self, rows: list[dict]) -> list[GlucosePoint]:
        parsed: list[GlucosePoint] = []
        for row in rows:
            ts = _extract_ts(row)
            if not ts:
                continue
            raw = row.get("sgv", row.get("mbg"))
            if raw is None:
                continue
            try:
                parsed.append(GlucosePoint(ts=ts, mgdl=float(raw)))
            except Exception:
                continue
        parsed.sort(key=lambda item: item.ts)
        return parsed

    def _parse_treatments(self, rows: list[dict]) -> tuple[list[CarbEvent], list[InsulinEvent]]:
        carbs: list[CarbEvent] = []
        insulin: list[InsulinEvent] = []
        for row in rows:
            ts = _extract_ts(row)
            if not ts:
                continue

            raw_carbs = row.get("carbs")
            if raw_carbs is not None:
                try:
                    grams = float(raw_carbs)
                    if grams > 0:
                        carbs.append(CarbEvent(ts=ts, grams=grams))
                except Exception:
                    pass

            raw_insulin = row.get("insulin")
            if raw_insulin is not None:
                try:
                    units = float(raw_insulin)
                    if units > 0:
                        insulin.append(InsulinEvent(ts=ts, units=units, kind="bolus"))
                except Exception:
                    pass
        carbs.sort(key=lambda item: item.ts)
        insulin.sort(key=lambda item: item.ts)
        return carbs, insulin


class SyntheticDataSource:
    def __init__(self, seed: int = 7) -> None:
        self.seed = seed

    async def fetch_dataset(self, *, patient_id: str, since: datetime, until: datetime) -> PatientDataset:
        random_seed = hash((self.seed, patient_id, since.date().isoformat(), until.date().isoformat()))
        rnd = random.Random(random_seed)

        glucose: list[GlucosePoint] = []
        carbs: list[CarbEvent] = []
        insulin: list[InsulinEvent] = []

        cursor = since
        meal_times = [8, 13, 19]
        while cursor <= until:
            day_fraction = (cursor.hour * 60 + cursor.minute) / (24 * 60)
            circadian = 12 * math.sin(2 * math.pi * day_fraction - 1.2)
            baseline = 118 + circadian
            noise = rnd.gauss(0, 9)
            glucose.append(GlucosePoint(ts=cursor, mgdl=max(55, min(280, baseline + noise))))
            cursor += timedelta(minutes=5)

        day_cursor = datetime(since.year, since.month, since.day, tzinfo=since.tzinfo)
        while day_cursor < until:
            for hour in meal_times:
                meal_ts = day_cursor + timedelta(hours=hour, minutes=rnd.randint(-20, 20))
                if since <= meal_ts <= until:
                    grams = max(20.0, min(95.0, rnd.gauss(52, 14)))
                    carbs.append(CarbEvent(ts=meal_ts, grams=grams))
                    insulin_units = max(1.0, grams / rnd.uniform(8.0, 14.0))
                    insulin.append(InsulinEvent(ts=meal_ts - timedelta(minutes=12), units=insulin_units, kind="bolus"))

            if rnd.random() > 0.4:
                correction_ts = day_cursor + timedelta(hours=rnd.randint(0, 23), minutes=rnd.randint(0, 59))
                if since <= correction_ts <= until:
                    insulin.append(InsulinEvent(ts=correction_ts, units=rnd.uniform(0.8, 2.4), kind="bolus"))

            day_cursor += timedelta(days=1)

        carbs.sort(key=lambda item: item.ts)
        insulin.sort(key=lambda item: item.ts)
        return PatientDataset(glucose=glucose, carbs=carbs, insulin=insulin)


def _extract_ts(row: dict) -> datetime | None:
    raw = row.get("dateString") or row.get("created_at")
    if raw:
        try:
            return datetime.fromisoformat(str(raw).replace("Z", "+00:00"))
        except Exception:
            pass

    if row.get("date"):
        try:
            return datetime.fromtimestamp(float(row["date"]) / 1000, tz=UTC)
        except Exception:
            return None
    return None
