package com.contentplatform.app.ui.auth

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import com.contentplatform.app.BuildConfig
import com.contentplatform.app.auth.AuthStore
import com.contentplatform.app.auth.PasswordPolicy
import com.contentplatform.app.dm.SessionStore
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

/**
 * Entry gate. The product is account-based: discovery, messaging, subscriptions and
 * creator tools all operate on the authenticated session. There is no anonymous demo mode.
 */
@Composable
fun AuthScreen(store: AuthStore) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var registerMode by rememberSaveable { mutableStateOf(false) }
    var baseUrl by rememberSaveable { mutableStateOf(BuildConfig.API_BASE_URL) }
    var email by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var mfa by rememberSaveable { mutableStateOf("") }
    var resetStage by rememberSaveable { mutableStateOf(0) } // 0 sign-in, 1 request, 2 confirm
    var resetToken by rememberSaveable { mutableStateOf("") }
    var newPassword by rememberSaveable { mutableStateOf("") }

    Column(
        Modifier.fillMaxSize().background(palette.ground).imePadding().padding(24.dp),
        verticalArrangement = androidx.compose.foundation.layout.Arrangement.Center,
    ) {
        LabelMono("18+ PLATFORM")
        Text("Vesper", style = MaterialTheme.typography.displayMedium, color = palette.ink)
        Text(
            when {
                resetStage == 1 -> "Reset your password"
                resetStage == 2 -> "Enter the reset code"
                registerMode -> "Create an account"
                else -> "Sign in to continue"
            },
            style = MaterialTheme.typography.bodyMedium,
            color = palette.muted,
        )
        SpacerH(18)
        CardSurface {
            Column(Modifier.padding(16.dp)) {
                if (BuildConfig.DEBUG) {
                    Field("Server", baseUrl, { baseUrl = it }, keyboardType = KeyboardType.Uri)
                    SpacerH(10)
                }
                if (resetStage == 0) {
                    Field("Email", email, { email = it }, keyboardType = KeyboardType.Email)
                    SpacerH(10)
                    Field("Password", password, { password = it }, password = true)
                    if (registerMode) {
                        SpacerH(4)
                        val problem = PasswordPolicy.problem(password)
                        Text(
                            problem ?: "At least ${PasswordPolicy.MinBytes} characters.",
                            color = if (password.isNotEmpty() && problem != null) palette.danger else palette.muted,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                    if (!registerMode && store.mfaRequired) {
                        SpacerH(10)
                        Field("Authenticator code", mfa, { mfa = it.take(6) }, keyboardType = KeyboardType.Number)
                    }
                    store.error?.let {
                        SpacerH(8)
                        Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                    }
                    SpacerH(14)
                    ActionButton(
                        label = when {
                            store.busy -> "Working…"
                            registerMode -> "Create account"
                            else -> "Sign in"
                        },
                        price = null,
                        onClick = {
                            scope.launch {
                                if (registerMode) store.register(baseUrl, email, password)
                                else store.login(baseUrl, email, password, mfa)
                            }
                        },
                        enabled = !store.busy && email.isNotBlank() && password.isNotEmpty() &&
                            baseUrl.isNotBlank() &&
                            (!registerMode || PasswordPolicy.problem(password) == null),
                    )
                    SpacerH(8)
                    OutlineButton(
                        label = if (registerMode) "I already have an account" else "Create an account",
                        onClick = {
                            registerMode = !registerMode
                            store.clearError()
                        },
                    )
                    if (!registerMode) {
                        SpacerH(8)
                        OutlineButton(
                            label = "Forgot password?",
                            onClick = {
                                resetStage = 1
                                store.clearReset()
                            },
                        )
                    }
                } else if (resetStage == 1) {
                    Field("Email", email, { email = it }, keyboardType = KeyboardType.Email)
                    store.resetNotice?.let {
                        SpacerH(8)
                        Text(it, color = palette.actionText, style = MaterialTheme.typography.bodySmall)
                    }
                    store.resetError?.let {
                        SpacerH(8)
                        Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                    }
                    SpacerH(14)
                    ActionButton(
                        label = if (store.resetBusy) "Sending…" else "Send reset code",
                        price = null,
                        onClick = { scope.launch { store.requestPasswordReset(baseUrl, email) } },
                        enabled = !store.resetBusy && email.isNotBlank() && baseUrl.isNotBlank(),
                    )
                    SpacerH(8)
                    OutlineButton(
                        label = "I have a reset code",
                        onClick = { resetStage = 2; store.clearReset() },
                    )
                    SpacerH(8)
                    OutlineButton(
                        label = "Back to sign in",
                        onClick = { resetStage = 0; store.clearReset() },
                    )
                } else {
                    Field("Reset code", resetToken, { resetToken = it }, keyboardType = KeyboardType.Text)
                    SpacerH(10)
                    Field("New password", newPassword, { newPassword = it }, password = true)
                    store.resetNotice?.let {
                        SpacerH(8)
                        Text(it, color = palette.actionText, style = MaterialTheme.typography.bodySmall)
                    }
                    store.resetError?.let {
                        SpacerH(8)
                        Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                    }
                    SpacerH(14)
                    ActionButton(
                        label = if (store.resetBusy) "Resetting…" else "Reset password",
                        price = null,
                        onClick = {
                            scope.launch {
                                if (store.confirmPasswordReset(baseUrl, resetToken, newPassword)) {
                                    password = ""
                                    resetToken = ""
                                    newPassword = ""
                                    resetStage = 0
                                }
                            }
                        },
                        enabled = !store.resetBusy && resetToken.isNotBlank() && newPassword.isNotEmpty() && baseUrl.isNotBlank(),
                    )
                    SpacerH(8)
                    OutlineButton(
                        label = "Back to sign in",
                        onClick = { resetStage = 0; store.clearReset() },
                    )
                }
            }
        }
        SpacerH(14)
        Text(
            "Use https for a deployed server. The default local address is only for development.",
            style = MaterialTheme.typography.bodySmall,
            color = palette.muted,
        )
    }
}

@Composable
private fun Field(
    label: String,
    value: String,
    onChange: (String) -> Unit,
    password: Boolean = false,
    keyboardType: KeyboardType = KeyboardType.Text,
) {
    val palette = LocalVesperPalette.current
    Column {
        LabelMono(label)
        SpacerH(4)
        Box(
            Modifier.fillMaxWidth().height(46.dp)
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
                visualTransformation = if (password) PasswordVisualTransformation() else VisualTransformation.None,
                keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = ImeAction.Next),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
