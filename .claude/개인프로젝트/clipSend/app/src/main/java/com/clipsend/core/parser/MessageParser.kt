package com.clipsend.core.parser

import com.clipsend.core.model.ParsedMessage

object MessageParser {

    fun parse(rawText: String): ParsedMessage? {
        val trimmed = rawText.trim('\n', '\r')
        if (trimmed.isBlank()) return null

        val lines = trimmed.lines()
        val title = lines.first().trim().ifBlank { "(제목 없음)" }
        val body = lines.drop(1).joinToString("\n").trim()

        return ParsedMessage(title = title, body = body)
    }
}