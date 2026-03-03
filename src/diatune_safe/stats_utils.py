from __future__ import annotations

from statistics import median


def robust_median(values: list[float]) -> float | None:
    if not values:
        return None
    return float(median(values))


def mad(values: list[float]) -> float | None:
    if not values:
        return None
    med = median(values)
    abs_dev = [abs(value - med) for value in values]
    return float(median(abs_dev))


def winsorized(values: list[float], z_limit: float = 3.5) -> list[float]:
    if len(values) < 4:
        return values[:]
    med = robust_median(values)
    mad_value = mad(values)
    if med is None or mad_value is None or mad_value == 0:
        return values[:]
    robust_sigma = mad_value * 1.4826
    lower = med - z_limit * robust_sigma
    upper = med + z_limit * robust_sigma
    return [min(max(item, lower), upper) for item in values]


def robust_mean(values: list[float]) -> float | None:
    if not values:
        return None
    clipped = winsorized(values)
    return float(sum(clipped) / len(clipped))
