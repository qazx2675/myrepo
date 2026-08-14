package com.clipsend.data.repository

import com.clipsend.core.model.ParsedMessage
import com.clipsend.core.parser.MessageParser
import com.clipsend.data.db.HistoryDao
import com.clipsend.data.db.HistoryEntity
import com.clipsend.data.gmail.GmailSender
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class SendRepository(
    private val historyDao: HistoryDao
) {
    data class CaptureResult(
        val parsed: ParsedMessage?,
        val sendResult: GmailSender.SendResult?
    )

    /**
     * Parses raw captured text, records it as a history entry, and — when both a recipient
     * and a sender (email + app password) are configured — sends it via Gmail SMTP right away.
     */
    suspend fun captureAndRecord(
        rawText: String,
        recipientEmail: String,
        senderEmail: String,
        senderAppPassword: String
    ): CaptureResult {
        val parsed = MessageParser.parse(rawText) ?: return CaptureResult(null, null)

        val entity = HistoryEntity(
            channel = if (recipientEmail.isBlank()) "UNSET" else "GMAIL",
            title = parsed.title,
            body = parsed.body,
            recipient = recipientEmail,
            timestamp = System.currentTimeMillis(),
            status = "PENDING"
        )
        val id = historyDao.insert(entity)

        if (recipientEmail.isBlank() || senderEmail.isBlank() || senderAppPassword.isBlank()) {
            historyDao.update(
                entity.copy(
                    id = id,
                    status = "FAILED",
                    errorMessage = "수신자 또는 발신 계정이 설정되지 않았습니다"
                )
            )
            return CaptureResult(parsed, null)
        }

        val result = withContext(Dispatchers.IO) {
            GmailSender.send(senderEmail, senderAppPassword, recipientEmail, parsed.title, parsed.body)
        }

        historyDao.update(
            if (result.success) {
                entity.copy(id = id, status = "SUCCESS")
            } else {
                entity.copy(id = id, status = "FAILED", errorMessage = result.errorMessage)
            }
        )

        return CaptureResult(parsed, result)
    }
}
