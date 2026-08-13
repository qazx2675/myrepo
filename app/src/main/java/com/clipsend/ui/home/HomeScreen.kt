package com.clipsend.ui.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

@Composable
fun HomeScreen(
    recipientEmail: String,
    onStartOverlay: () -> Unit,
    onStopOverlay: () -> Unit,
    onOpenSettings: () -> Unit
) {
    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center
    ) {
        Text(
            if (recipientEmail.isBlank()) "받는 사람: 설정되지 않음"
            else "받는 사람: $recipientEmail"
        )
        Spacer(modifier = Modifier.height(16.dp))
        Button(onClick = onStartOverlay) {
            Text("오버레이 시작")
        }
        Spacer(modifier = Modifier.height(8.dp))
        OutlinedButton(onClick = onStopOverlay) {
            Text("오버레이 중지")
        }
        Spacer(modifier = Modifier.height(16.dp))
        TextButton(onClick = onOpenSettings) {
            Text("설정")
        }
    }
}
