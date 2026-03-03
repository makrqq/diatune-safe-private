from __future__ import annotations

import argparse
import asyncio
import logging

import uvicorn

from diatune_safe.api import create_app
from diatune_safe.config import get_settings
from diatune_safe.scheduler import AnalysisScheduler
from diatune_safe.service import AnalysisService
from diatune_safe.telegram_bot import TelegramBotRunner


def _setup_logging(level: str) -> None:
    logging.basicConfig(
        level=getattr(logging, level.upper(), logging.INFO),
        format="%(asctime)s %(levelname)s %(name)s - %(message)s",
    )


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Командная строка Diatune Safe")
    subparsers = parser.add_subparsers(dest="command", required=True)

    api_parser = subparsers.add_parser("api", help="Запустить FastAPI-сервер")
    api_parser.add_argument("--host", default=None)
    api_parser.add_argument("--port", type=int, default=None)

    subparsers.add_parser("bot", help="Запустить Telegram-бота")

    worker_parser = subparsers.add_parser("worker", help="Запустить фоновый планировщик")
    worker_parser.add_argument("--patients", default="", help="ID пациентов через запятую")

    analyze_parser = subparsers.add_parser("analyze", help="Выполнить разовый анализ")
    analyze_parser.add_argument("--patient-id", required=True)
    analyze_parser.add_argument("--days", type=int, default=None)
    analyze_parser.add_argument("--synthetic", action="store_true")

    bootstrap_parser = subparsers.add_parser("bootstrap", help="Создать профиль пациента по умолчанию")
    bootstrap_parser.add_argument("--patient-id", required=True)

    return parser


async def _run_worker(service: AnalysisService, patients: list[str]) -> None:
    scheduler = AnalysisScheduler(get_settings(), service)
    await scheduler.run_forever(patient_ids=patients)


async def _run_analyze(service: AnalysisService, patient_id: str, days: int | None, synthetic: bool) -> None:
    report = await service.run_analysis(patient_id=patient_id, days=days, prefer_real_data=not synthetic)
    print(f"ID запуска: {report.run_id}")
    print(f"Пациент: {report.patient_id}")
    print(f"Период: {report.period_start.isoformat()} - {report.period_end.isoformat()}")
    print(f"Предупреждений: {len(report.warnings)}")
    for warning in report.warnings:
        print(f"- {warning}")
    print("Рекомендации:")
    for rec in report.recommendations:
        status = "ЗАБЛОКИРОВАНО" if rec.blocked else "К ВЫПОЛНЕНИЮ"
        print(
            f"  [{status}] #{rec.id} {rec.block_name} {rec.parameter.upper()}: "
            f"{rec.current_value:.2f} -> {rec.proposed_value:.2f} ({rec.percent_change:+.1f}%)"
        )


def main() -> None:
    parser = _build_parser()
    args = parser.parse_args()
    settings = get_settings()
    _setup_logging(settings.log_level)
    service = AnalysisService(settings)

    if args.command == "api":
        app = create_app(settings=settings, service=service)
        uvicorn.run(
            app,
            host=args.host or settings.app_host,
            port=args.port or settings.app_port,
            log_level=settings.log_level.lower(),
        )
        return

    if args.command == "bot":
        runner = TelegramBotRunner(settings, service)
        runner.run()
        return

    if args.command == "worker":
        patient_ids = [item.strip() for item in args.patients.split(",") if item.strip()]
        asyncio.run(_run_worker(service, patient_ids))
        return

    if args.command == "analyze":
        asyncio.run(_run_analyze(service, args.patient_id, args.days, args.synthetic))
        return

    if args.command == "bootstrap":
        profile = service.get_profile(args.patient_id)
        print(f"Профиль готов для patient_id={profile.patient_id}, блоков={len(profile.blocks)}")
        return


if __name__ == "__main__":
    main()
