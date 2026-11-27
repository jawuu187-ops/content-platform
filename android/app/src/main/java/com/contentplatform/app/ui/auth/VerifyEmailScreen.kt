package com.contentplatform.app.ui.auth

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.contentplatform.app.auth.AuthStore
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

/**
 * Shown once after registration. It does not block the product: postponing keeps the
 * account usable and the same step remains in Account security.
 */
@Composable
fun VerifyEmailScreen(store: AuthStore, onPostpone: () -> Unit) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var code by remember { mutableStateOf("") }

    Column(
        Modifier.fillMaxSize().background(palette.ground).imePadding().padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        LabelMono("ONE STEP LEFT")
        Text("Confirm your email", style = MaterialTheme.typography.displaySmall, color = palette.ink)
        Text(
            "We send a six-character code to " + (store.pendingVerificationEmail ?: "your address") +
                ". It can be used once and expires in 30 minutes.",
            style = MaterialTheme.typography.bodyMedium,
            color = palette.muted,
        )
        SpacerH(18)
        CardSurface {
            Column(Modifier.padding(16.dp)) {
                LabelMono("Confirmation code")
                SpacerH(4)
                Box(
                    Modifier.fillMaxWidth().height(46.dp)
                        .background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp))
                        .padding(horizontal = 12.dp),
                    contentAlignment = Alignment.CenterStart,
                ) {
                    BasicTextField(
                        value = code,
                        onValueChange = { code = it.uppercase().take(12) },
                        singleLine = true,
                        textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                        cursorBrush = SolidColor(palette.ink),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Text),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                store.verificationNotice?.let {
                    SpacerH(10)
                    Text(it, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
                store.verificationError?.let {
                    SpacerH(10)
                    Text(it, style = MaterialTheme.typography.bodySmall, color = palette.danger)
                }
                SpacerH(14)
                ActionButton(
                    label = if (store.verificationBusy) "Checking…" else "Confirm email",
                    price = null,
                    onClick = {
                        scope.launch {
                            if (store.confirmVerification(code)) onPostpone()
                        }
                    },
                    enabled = !store.verificationBusy && code.isNotBlank(),
                )
                SpacerH(8)
                OutlineButton(
                    label = if (store.verificationBusy) "Sending…" else "Send a new code",
                    onClick = { scope.launch { store.sendVerificationEmail() } },
                )
                SpacerH(8)
                OutlineButton(label = "Verify later", onClick = onPostpone)
            }
        }
        SpacerH(14)
        Text(
            "An unconfirmed address can be fixed later from You → Account security.",
            style = MaterialTheme.typography.bodySmall,
            color = palette.muted,
        )
    }
}
