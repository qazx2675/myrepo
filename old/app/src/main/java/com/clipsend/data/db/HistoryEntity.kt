package com.clipsend.data.db

import androidx.room.Entity
import androidx.room.PrimaryKey

@Entity(tableName = "send_history")
data class HistoryEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val channel: String,        // "GMAIL" / "DISCORD" / "UNSET"
    val title: String,
    val body: String,
    val recipient: String,      // 이메일 주소 or Discord 채널명, 미설정 시 빈 문자열
    val timestamp: Long,
    val status: String,         // SUCCESS / FAILED / PENDING
    val errorMessage: String? = null
)