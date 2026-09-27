package com.clipsend

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import com.clipsend.data.prefs.SettingsRepository
import com.clipsend.overlay.OverlayService
import com.clipsend.ui.home.HomeScreen
import com.clipsend.ui.settings.SettingsScreen
import com.clipsend.ui.theme.ClipSendTheme
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {

    private val settingsRepository: SettingsRepository by lazy { (application as App).settingsRepository }

    private var recipientEmail by mutableStateOf("")
    private var senderEmail by mutableStateOf("")
    private var senderAppPassword by mutableStateOf("")
    private var showSettings by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        lifecycleScope.launch {
            recipientEmail = settingsRepository.recipientEmail.first()
        }
        lifecycleScope.launch {
            senderEmail = settingsRepository.senderEmail.first()
        }
        lifecycleScope.launch {
            senderAppPassword = settingsRepository.senderAppPassword.first()
        }

        setContent {
            ClipSendTheme {
                val overlayPermissionLauncher = rememberLauncherForActivityResult(
                    contract = ActivityResultContracts.StartActivityForResult()
                ) {
                    if (hasOverlayPermission()) {
                        startOverlayService()
                    } else {
                        Toast.makeText(this@MainActivity, "다른 앱 위에 표시 권한이 필요합니다", Toast.LENGTH_SHORT).show()
                    }
                }

                if (showSettings) {
                    SettingsScreen(
                        recipientEmail = recipientEmail,
                        senderEmail = senderEmail,
                        senderAppPassword = senderAppPassword,
                        onSaveRecipientEmail = { email -> saveRecipientEmail(email) },
                        onSaveSender = { email, appPassword -> saveSender(email, appPassword) },
                        onBack = { showSettings = false }
                    )
                } else {
                    HomeScreen(
                        recipientEmail = recipientEmail,
                        onStartOverlay = {
                            if (hasOverlayPermission()) {
                                startOverlayService()
                            } else {
                                val intent = Intent(
                                    Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                                    Uri.parse("package:$packageName")
                                )
                                overlayPermissionLauncher.launch(intent)
                            }
                        },
                        onStopOverlay = { stopOverlayService() },
                        onOpenSettings = { showSettings = true }
                    )
                }
            }
        }
    }

    private fun hasOverlayPermission(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.M || Settings.canDrawOverlays(this)

    private fun startOverlayService() {
        val intent = Intent(this, OverlayService::class.java)
        ContextCompat.startForegroundService(this, intent)
        Toast.makeText(this, "오버레이 시작됨", Toast.LENGTH_SHORT).show()
        moveTaskToBack(true)
    }

    private fun stopOverlayService() {
        val intent = Intent(this, OverlayService::class.java).apply {
            action = OverlayService.ACTION_STOP
        }
        startService(intent)
    }

    private fun saveRecipientEmail(email: String) {
        lifecycleScope.launch {
            settingsRepository.setRecipientEmail(email)
            recipientEmail = email
            showSettings = false
            Toast.makeText(this@MainActivity, "저장됨", Toast.LENGTH_SHORT).show()
        }
    }

    private fun saveSender(email: String, appPassword: String) {
        lifecycleScope.launch {
            settingsRepository.setSenderEmail(email)
            settingsRepository.setSenderAppPassword(appPassword)
            senderEmail = email
            senderAppPassword = appPassword
            showSettings = false
            Toast.makeText(this@MainActivity, "저장됨", Toast.LENGTH_SHORT).show()
        }
    }
}
