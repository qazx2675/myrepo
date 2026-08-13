package com.clipsend.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp

@Composable
fun SettingsScreen(
    recipientEmail: String,
    senderEmail: String,
    senderAppPassword: String,
    onSaveRecipientEmail: (String) -> Unit,
    onSaveSender: (email: String, appPassword: String) -> Unit,
    onBack: () -> Unit
) {
    var email by remember(recipientEmail) { mutableStateOf(recipientEmail) }
    var fromEmail by remember(senderEmail) { mutableStateOf(senderEmail) }
    var appPassword by remember(senderAppPassword) { mutableStateOf(senderAppPassword) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center
    ) {
        Text("Gmail 수신자 설정")
        Spacer(modifier = Modifier.height(16.dp))
        OutlinedTextField(
            value = email,
            onValueChange = { email = it },
            label = { Text("받는 사람 Gmail 주소") },
            placeholder = { Text("여러 명은 쉼표(,)로 구분") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
            modifier = Modifier.fillMaxWidth()
        )
        Spacer(modifier = Modifier.height(4.dp))
        Text("예: a@gmail.com, b@gmail.com", style = MaterialTheme.typography.labelSmall)
        Spacer(modifier = Modifier.height(16.dp))
        Button(
            onClick = { onSaveRecipientEmail(email.trim()) },
            modifier = Modifier.fillMaxWidth()
        ) {
            Text("저장")
        }

        Spacer(modifier = Modifier.height(24.dp))
        HorizontalDivider()
        Spacer(modifier = Modifier.height(24.dp))

        Text("보내는 사람 (Gmail 주소 + 앱 비밀번호)")
        Spacer(modifier = Modifier.height(8.dp))
        OutlinedTextField(
            value = fromEmail,
            onValueChange = { fromEmail = it },
            label = { Text("보내는 사람 Gmail 주소") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
            modifier = Modifier.fillMaxWidth()
        )
        Spacer(modifier = Modifier.height(8.dp))
        OutlinedTextField(
            value = appPassword,
            onValueChange = { appPassword = it },
            label = { Text("앱 비밀번호 (16자리)") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            modifier = Modifier.fillMaxWidth()
        )
        Spacer(modifier = Modifier.height(16.dp))
        Button(
            onClick = { onSaveSender(fromEmail.trim(), appPassword.trim()) },
            modifier = Modifier.fillMaxWidth()
        ) {
            Text("저장")
        }

        Spacer(modifier = Modifier.height(24.dp))
        TextButton(onClick = onBack) {
            Text("뒤로")
        }
    }
}
