package com.clipsend.overlay

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt

@Composable
fun OverlayBubbleContent(
    expanded: Boolean,
    onDrag: (dx: Int, dy: Int) -> Unit,
    onTap: () -> Unit,
    onTextCaptured: (String) -> Unit,
    onCancel: () -> Unit,
    onClose: () -> Unit
) {
    if (expanded) {
        PasteCard(onTextCaptured = onTextCaptured, onCancel = onCancel, onClose = onClose)
    } else {
        CollapsedBubble(onDrag = onDrag, onTap = onTap)
    }
}

@Composable
private fun CollapsedBubble(onDrag: (Int, Int) -> Unit, onTap: () -> Unit) {
    Box(
        modifier = Modifier
            .size(48.dp)
            .background(MaterialTheme.colorScheme.primary, CircleShape)
            .pointerInput(Unit) { detectTapGestures(onTap = { onTap() }) }
            .pointerInput(Unit) {
                detectDragGestures { change, dragAmount ->
                    change.consume()
                    onDrag(dragAmount.x.roundToInt(), dragAmount.y.roundToInt())
                }
            },
        contentAlignment = Alignment.Center
    ) {
        Text("붙", color = Color.White, style = MaterialTheme.typography.titleMedium)
    }
}

@Composable
private fun PasteCard(
    onTextCaptured: (String) -> Unit,
    onCancel: () -> Unit,
    onClose: () -> Unit
) {
    var text by remember { mutableStateOf("") }
    val focusRequester = remember { FocusRequester() }

    LaunchedEffect(text) {
        if (text.isNotBlank()) onTextCaptured(text)
    }
    LaunchedEffect(Unit) {
        focusRequester.requestFocus()
    }

    Card(elevation = CardDefaults.cardElevation(8.dp), modifier = Modifier.width(260.dp)) {
        Column(modifier = Modifier.padding(12.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("붙여넣기", style = MaterialTheme.typography.titleSmall)
                Text(
                    "✕",
                    modifier = Modifier
                        .padding(4.dp)
                        .pointerInput(Unit) { detectTapGestures(onTap = { onClose() }) },
                    color = MaterialTheme.colorScheme.error
                )
            }
            Spacer(modifier = Modifier.height(8.dp))
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focusRequester),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done)
            )
            Spacer(modifier = Modifier.height(8.dp))
            TextButton(
                onClick = onCancel,
                modifier = Modifier.align(Alignment.End)
            ) {
                Text("취소")
            }
        }
    }
}
