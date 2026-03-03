from __future__ import annotations

from telegram import Update
from telegram.ext import Application, CommandHandler, ContextTypes

from diatune_safe.config import AppSettings
from diatune_safe.domain import AnalysisReport, Recommendation
from diatune_safe.service import AnalysisService


def _is_allowed(settings: AppSettings, tg_user_id: int | None) -> bool:
    if not settings.telegram_allowed_user_ids:
        return True
    if tg_user_id is None:
        return False
    return tg_user_id in settings.telegram_allowed_user_ids


class TelegramBotRunner:
    def __init__(self, settings: AppSettings, service: AnalysisService) -> None:
        self.settings = settings
        self.service = service

    def run(self) -> None:
        if not self.settings.telegram_bot_token:
            raise RuntimeError("TELEGRAM_BOT_TOKEN is empty.")

        app = Application.builder().token(self.settings.telegram_bot_token).build()
        app.add_handler(CommandHandler("start", self._start))
        app.add_handler(CommandHandler("help", self._help))
        app.add_handler(CommandHandler("analyze", self._analyze))
        app.add_handler(CommandHandler("latest", self._latest))
        app.add_handler(CommandHandler("pending", self._pending))
        app.add_handler(CommandHandler("ack", self._ack))
        app.run_polling()

    async def _start(self, update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_allowed(self.settings, update.effective_user.id if update.effective_user else None):
            await update.effective_message.reply_text("Доступ запрещен.")
            return
        await update.effective_message.reply_text(
            "Diatune Safe Bot\n"
            "Команды:\n"
            "/analyze [patient_id] [days]\n"
            "/latest [patient_id]\n"
            "/pending [patient_id]\n"
            "/ack <recommendation_id> [reviewer]\n"
            "\nСервис только предлагает изменения и никогда не применяет их автоматически."
        )

    async def _help(self, update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        await self._start(update, context)

    async def _analyze(self, update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_allowed(self.settings, update.effective_user.id if update.effective_user else None):
            await update.effective_message.reply_text("Доступ запрещен.")
            return
        patient_id = context.args[0] if context.args else f"tg-{update.effective_user.id}"
        try:
            days = int(context.args[1]) if len(context.args) > 1 else self.settings.analysis_lookback_days
        except ValueError:
            await update.effective_message.reply_text("Неверный формат days. Пример: /analyze demo 14")
            return
        try:
            report = await self.service.run_analysis(patient_id=patient_id, days=days, prefer_real_data=True)
            await update.effective_message.reply_text(self._format_report(report))
        except Exception as exc:
            await update.effective_message.reply_text(f"Ошибка анализа: {exc}")

    async def _latest(self, update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_allowed(self.settings, update.effective_user.id if update.effective_user else None):
            await update.effective_message.reply_text("Доступ запрещен.")
            return
        patient_id = context.args[0] if context.args else f"tg-{update.effective_user.id}"
        report = self.service.get_latest_report(patient_id)
        if not report:
            await update.effective_message.reply_text("Отчеты не найдены. Выполните /analyze.")
            return
        await update.effective_message.reply_text(self._format_report(report))

    async def _pending(self, update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_allowed(self.settings, update.effective_user.id if update.effective_user else None):
            await update.effective_message.reply_text("Доступ запрещен.")
            return
        patient_id = context.args[0] if context.args else f"tg-{update.effective_user.id}"
        pending = self.service.list_pending_recommendations(patient_id)
        if not pending:
            await update.effective_message.reply_text("Нет ожидающих подтверждения рекомендаций.")
            return
        lines = ["Pending recommendations:"]
        for rec in pending[:20]:
            lines.append(_format_recommendation(rec))
        await update.effective_message.reply_text("\n".join(lines))

    async def _ack(self, update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        if not _is_allowed(self.settings, update.effective_user.id if update.effective_user else None):
            await update.effective_message.reply_text("Доступ запрещен.")
            return
        if not context.args:
            await update.effective_message.reply_text("Использование: /ack <recommendation_id> [reviewer]")
            return
        try:
            recommendation_id = int(context.args[0])
        except ValueError:
            await update.effective_message.reply_text("recommendation_id должен быть числом.")
            return
        reviewer = context.args[1] if len(context.args) > 1 else f"tg:{update.effective_user.id}"
        ok = self.service.acknowledge_recommendation(recommendation_id, reviewer)
        if ok:
            await update.effective_message.reply_text(f"Рекомендация {recommendation_id} подтверждена ({reviewer}).")
        else:
            await update.effective_message.reply_text("Рекомендация не найдена или уже подтверждена.")

    def _format_report(self, report: AnalysisReport) -> str:
        generated = report.generated_at.astimezone().strftime("%Y-%m-%d %H:%M")
        lines = [
            f"Report #{report.run_id} [{report.patient_id}]",
            f"Generated: {generated}",
            f"Period: {report.period_start.date()}..{report.period_end.date()}",
            f"Global hypos: {report.global_hypo_events}",
        ]
        if report.warnings:
            lines.append("Warnings:")
            lines.extend(f"- {item}" for item in report.warnings)
        lines.append("Recommendations:")
        for rec in report.recommendations:
            lines.append(_format_recommendation(rec))
        lines.append("Важно: это только предложения, ручное решение обязательно.")
        return "\n".join(lines)


def _format_recommendation(rec: Recommendation) -> str:
    status = "BLOCKED" if rec.blocked else "OPEN"
    sign = "+" if rec.percent_change > 0 else ""
    line = (
        f"#{rec.id or '-'} {status} {rec.block_name} {rec.parameter.upper()}: "
        f"{rec.current_value:.2f} -> {rec.proposed_value:.2f} ({sign}{rec.percent_change:.1f}%, conf={rec.confidence:.2f})"
    )
    if rec.blocked_reason:
        line = f"{line} | {rec.blocked_reason}"
    if rec.rationale:
        line = f"{line} | {'; '.join(rec.rationale[:2])}"
    return line
