package com.clipsend

import android.app.Application
import androidx.room.Room
import com.clipsend.data.db.AppDatabase
import com.clipsend.data.prefs.SettingsRepository
import com.clipsend.data.repository.SendRepository

/**
 * Minimal manual DI container. No framework (Hilt) is used here since the
 * currently pinned AGP version isn't yet compatible with the Hilt Gradle plugin.
 */
class App : Application() {

    val database: AppDatabase by lazy {
        Room.databaseBuilder(this, AppDatabase::class.java, "clipsend.db").build()
    }

    val settingsRepository: SettingsRepository by lazy {
        SettingsRepository(applicationContext)
    }

    val sendRepository: SendRepository by lazy {
        SendRepository(database.historyDao())
    }
}