package com.clipsend.data.gmail

import java.util.Properties
import javax.mail.Authenticator
import javax.mail.Message
import javax.mail.PasswordAuthentication
import javax.mail.Session
import javax.mail.Transport
import javax.mail.internet.InternetAddress
import javax.mail.internet.MimeMessage

/**
 * Sends mail via Gmail's SMTP server using an app password
 * (Google Account -> Security -> 2-Step Verification -> App passwords).
 * No Cloud project / OAuth consent screen / re-login needed.
 */
object GmailSender {

    class SendResult private constructor(
        val success: Boolean,
        val errorMessage: String?
    ) {
        companion object {
            fun ok() = SendResult(true, null)
            fun failure(message: String) = SendResult(false, message)
        }
    }

    /** Performs blocking network I/O — call from a background dispatcher. */
    fun send(senderEmail: String, appPassword: String, recipient: String, subject: String, body: String): SendResult {
        return try {
            val props = Properties().apply {
                put("mail.smtp.auth", "true")
                put("mail.smtp.starttls.enable", "true")
                put("mail.smtp.host", "smtp.gmail.com")
                put("mail.smtp.port", "587")
            }

            val session = Session.getInstance(props, object : Authenticator() {
                override fun getPasswordAuthentication(): PasswordAuthentication =
                    PasswordAuthentication(senderEmail, appPassword)
            })

            val message = MimeMessage(session).apply {
                setFrom(InternetAddress(senderEmail))
                setRecipients(Message.RecipientType.TO, InternetAddress.parse(recipient))
                setSubject(subject, "UTF-8")
                setText(body, "UTF-8")
            }

            Transport.send(message)
            SendResult.ok()
        } catch (e: Exception) {
            SendResult.failure(e.message ?: "전송 실패")
        }
    }
}
