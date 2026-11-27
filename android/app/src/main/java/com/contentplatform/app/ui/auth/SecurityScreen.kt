package com.contentplatform.app.ui.auth

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.contentplatform.app.auth.SecurityStore
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

@Composable
fun SecurityScreen(store: SecurityStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    var code by remember { mutableStateOf("") }
    var mfaCode by remember { mutableStateOf("") }
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { store.load() }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).background(palette.chrome).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(48.dp).pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) { Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink) }
            Text("Account security", style = MaterialTheme.typography.titleMedium, color = palette.ink)
        }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(20.dp)) {
            item {
                Text(store.email, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                LabelMono(if (store.emailVerified) "EMAIL VERIFIED" else "EMAIL NOT VERIFIED")
                SpacerH(14)
                if (!store.emailVerified) {
                    CardSurface {
                        Column(Modifier.padding(14.dp)) {
                            Text("Verify your email", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                            SpacerH(6)
                            OutlineButton("Send verification code", onClick = {
                                scope.launch { store.requestEmail() }
                            })
                            SpacerH(8)
                            CodeField(code, { code = it })
                            SpacerH(8)
                            ActionButton("Confirm code", null, onClick = {
                                scope.launch { store.confirmEmail(code) }
                            }, enabled = !store.busy && code.isNotBlank())
                        }
                    }
                    SpacerH(14)
                }
                CardSurface {
                    Column(Modifier.padding(14.dp)) {
                        Text("Two-factor authentication", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                        SpacerH(4)
                        Text(
                            "Use an authenticator app. Recovery codes are shown once after confirmation.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                        SpacerH(8)
                        if (store.totpSecret == null) {
                            OutlineButton("Set up authenticator", onClick = { scope.launch { store.startTOTP() } })
                        } else {
                            Text("Secret", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                            Text(store.totpSecret.orEmpty(), style = MaterialTheme.typography.bodySmall, color = palette.ink)
                            Text(store.totpURI.orEmpty(), style = MaterialTheme.typography.bodySmall, color = palette.muted)
                            SpacerH(8)
                            CodeField(mfaCode, { mfaCode = it.take(6) })
                            SpacerH(8)
                            ActionButton("Confirm authenticator", null, onClick = {
                                scope.launch { store.confirmTOTP(mfaCode) }
                            }, enabled = !store.busy && mfaCode.length >= 6)
                        }
                        if (store.recoveryCodes.isNotEmpty()) {
                            SpacerH(10)
                            Text("Recovery codes", style = MaterialTheme.typography.titleMedium, color = palette.danger)
                            store.recoveryCodes.forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = palette.ink) }
                        }
                    }
                }
                store.message?.let {
                    SpacerH(10)
                    Text(it, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
                }
                SpacerH(18)
                OutlineButton("Sign out", onClick = { scope.launch { store.logout() } })
            }
        }
    }
}

@Composable
private fun CodeField(value: String, onChange: (String) -> Unit) {
    val palette = LocalVesperPalette.current
    Box(
        Modifier.fillMaxWidth().height(44.dp)
            .background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        BasicTextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
            cursorBrush = SolidColor(palette.ink),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
