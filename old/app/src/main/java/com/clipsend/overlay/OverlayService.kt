package com.clipsend.overlay

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.graphics.PixelFormat
import android.os.Build
import android.os.IBinder
import android.view.Gravity
import android.view.WindowManager
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.ComposeView
import androidx.core.app.NotificationCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.LifecycleRegistry
import androidx.lifecycle.ViewModelStore
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.setViewTreeLifecycleOwner
import androidx.lifecycle.setViewTreeViewModelStoreOwner
import androidx.savedstate.SavedStateRegistry
import androidx.savedstate.SavedStateRegistryController
import androidx.savedstate.SavedStateRegistryOwner
import androidx.savedstate.setViewTreeSavedStateRegistryOwner
import com.clipsend.App
import com.clipsend.MainActivity
import com.clipsend.R
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/**
 * Hosts a small always-on-top floating bubble (SYSTEM_ALERT_WINDOW) that replaces the old
 * PIP-based flow. Tapping the bubble expands it in place into a tiny paste card; pasting
 * captures the text, sends it, and collapses back to the bubble automatically.
 */
class OverlayService :
    Service(),
    LifecycleOwner,
    ViewModelStoreOwner,
    SavedStateRegistryOwner {

    private val lifecycleRegistry = LifecycleRegistry(this)
    override val lifecycle: Lifecycle get() = lifecycleRegistry
    override val viewModelStore = ViewModelStore()
    private val savedStateRegistryController = SavedStateRegistryController.create(this)
    override val savedStateRegistry: SavedStateRegistry get() = savedStateRegistryController.savedStateRegistry

    private val serviceScope = CoroutineScope(Dispatchers.Main + Job())

    private lateinit var windowManager: WindowManager
    private var composeView: ComposeView? = null
    private var layoutParams: WindowManager.LayoutParams? = null

    private var isExpanded by mutableStateOf(false)

    companion object {
        const val ACTION_STOP = "com.clipsend.overlay.action.STOP"
        private const val CHANNEL_ID = "overlay_channel"
        private const val NOTIFICATION_ID = 1001
        private const val COLLAPSED_SIZE_DP = 48
    }

    override fun onCreate() {
        super.onCreate()
        savedStateRegistryController.performRestore(null)
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_CREATE)
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_START)
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_RESUME)

        startForeground(NOTIFICATION_ID, buildNotification())
        windowManager = getSystemService(Context.WINDOW_SERVICE) as WindowManager
        addOverlay()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
        }
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private fun buildNotification(): android.app.Notification {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val manager = getSystemService(NotificationManager::class.java)
            val channel = NotificationChannel(
                CHANNEL_ID,
                "clipSend 오버레이",
                NotificationManager.IMPORTANCE_MIN
            )
            manager.createNotificationChannel(channel)
        }

        val openAppIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentTitle("clipSend 실행 중")
            .setContentText("떠 있는 버튼을 눌러 붙여넣기")
            .setContentIntent(openAppIntent)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .build()
    }

    private fun addOverlay() {
        val view = ComposeView(this).apply {
            setViewTreeLifecycleOwner(this@OverlayService)
            setViewTreeViewModelStoreOwner(this@OverlayService)
            setViewTreeSavedStateRegistryOwner(this@OverlayService)
        }
        composeView = view

        val density = resources.displayMetrics.density
        val collapsedPx = (COLLAPSED_SIZE_DP * density).toInt()

        val overlayType = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
        } else {
            @Suppress("DEPRECATION")
            WindowManager.LayoutParams.TYPE_PHONE
        }

        val params = WindowManager.LayoutParams(
            collapsedPx,
            collapsedPx,
            overlayType,
            WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE,
            PixelFormat.TRANSLUCENT
        ).apply {
            gravity = Gravity.TOP or Gravity.START
            x = resources.displayMetrics.widthPixels - collapsedPx - (16 * density).toInt()
            y = resources.displayMetrics.heightPixels / 3
        }
        layoutParams = params

        view.setContent {
            com.clipsend.ui.theme.ClipSendTheme {
                OverlayBubbleContent(
                    expanded = isExpanded,
                    onDrag = { dx, dy -> moveBy(dx, dy) },
                    onTap = { expand() },
                    onTextCaptured = { text ->
                        collapse()
                        handleCaptured(text)
                    },
                    onCancel = { collapse() },
                    onClose = { stopSelf() }
                )
            }
        }

        windowManager.addView(view, params)
    }

    private fun moveBy(dx: Int, dy: Int) {
        val params = layoutParams ?: return
        params.x += dx
        params.y += dy
        composeView?.let { runCatching { windowManager.updateViewLayout(it, params) } }
    }

    private fun expand() {
        if (isExpanded) return
        isExpanded = true
        val params = layoutParams ?: return
        params.width = WindowManager.LayoutParams.WRAP_CONTENT
        params.height = WindowManager.LayoutParams.WRAP_CONTENT
        params.flags = params.flags and WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE.inv()
        params.softInputMode = WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_VISIBLE
        composeView?.let { runCatching { windowManager.updateViewLayout(it, params) } }
    }

    private fun collapse() {
        if (!isExpanded) return
        isExpanded = false
        val density = resources.displayMetrics.density
        val collapsedPx = (COLLAPSED_SIZE_DP * density).toInt()
        val params = layoutParams ?: return
        params.width = collapsedPx
        params.height = collapsedPx
        params.flags = params.flags or WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
        composeView?.let { runCatching { windowManager.updateViewLayout(it, params) } }
    }

    private fun handleCaptured(rawText: String) {
        if (rawText.isBlank()) return
        val app = application as App
        serviceScope.launch {
            val settings = app.settingsRepository
            val recipientEmail = settings.recipientEmail.first()
            val senderEmail = settings.senderEmail.first()
            val senderAppPassword = settings.senderAppPassword.first()

            val result = app.sendRepository.captureAndRecord(
                rawText,
                recipientEmail,
                senderEmail,
                senderAppPassword
            )

            val message = when {
                result.parsed == null -> "내용을 읽지 못했습니다"
                result.sendResult == null -> "이력에 저장됨 (수신자/발신 계정 미설정): ${result.parsed.title}"
                result.sendResult.success -> "Gmail 전송 완료: ${result.parsed.title}"
                else -> "전송 실패: ${result.sendResult.errorMessage}"
            }
            android.widget.Toast.makeText(this@OverlayService, message, android.widget.Toast.LENGTH_SHORT).show()
        }
    }

    override fun onDestroy() {
        lifecycleRegistry.handleLifecycleEvent(Lifecycle.Event.ON_DESTROY)
        composeView?.let { runCatching { windowManager.removeView(it) } }
        composeView = null
        serviceScope.cancel()
        super.onDestroy()
    }
}
