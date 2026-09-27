package com.clipsend.data.prefs

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.dataStore by preferencesDataStore(name = "clipsend_settings")

class SettingsRepository(
    private val context: Context
) {
    private object Keys {
        val RECIPIENT_EMAIL = stringPreferencesKey("recipient_email")
        val SENDER_EMAIL = stringPreferencesKey("sender_email")
        val SENDER_APP_PASSWORD = stringPreferencesKey("sender_app_password")
    }

    val recipientEmail: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[Keys.RECIPIENT_EMAIL] ?: ""
    }

    suspend fun setRecipientEmail(email: String) {
        context.dataStore.edit { it[Keys.RECIPIENT_EMAIL] = email.trim() }
    }

    val senderEmail: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[Keys.SENDER_EMAIL] ?: ""
    }

    suspend fun setSenderEmail(email: String) {
        context.dataStore.edit { it[Keys.SENDER_EMAIL] = email.trim() }
    }

    val senderAppPassword: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[Keys.SENDER_APP_PASSWORD] ?: ""
    }

    suspend fun setSenderAppPassword(password: String) {
        context.dataStore.edit { it[Keys.SENDER_APP_PASSWORD] = password.trim() }
    }
}
