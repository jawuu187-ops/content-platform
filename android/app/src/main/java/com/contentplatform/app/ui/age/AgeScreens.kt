package com.contentplatform.app.ui.age

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.contentplatform.app.age.AgeReviewRequest
import com.contentplatform.app.age.AgeStore
import com.contentplatform.app.age.AgeVerification
import com.contentplatform.app.age.RejectReasons
import com.contentplatform.app.age.reasonLabel
import com.contentplatform.app.age.statusLabel
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

/**
 * The user's age-verification card.
 *
 * The copy is deliberately blunt: verification is a live presentation, and no copy of an ID
 * should ever be uploaded, emailed or messaged. The app has no upload or camera control for
 * this flow, and the server has no document column.
 */
@Composable
fun AgeVerificationCard(
    verification: AgeVerification?,
    review: AgeReviewRequest?,
    busy: Boolean,
    error: String?,
    onRequest: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    CardSurface {
        Column(Modifier.padding(16.dp)) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(
                        verification?.let { statusLabel(it.status) } ?: "Age check",
                        style = MaterialTheme.typography.titleMedium,
                        color = palette.ink,
                    )
                    Text(
                        if (verification?.verified == true) {
                            "Adult content is unlocked until " + (verification.expiresAt?.take(10) ?: "the next review")
                        } else {
                            "Adult content stays locked until a reviewer approves you."
                        },
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                }
                LabelMono("18+")
            }
            review?.let {
                SpacerH(10)
                Text(
                    "Manual review: ${statusLabel(it.status)}",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.inkSecondary,
                )
                if (it.status == "rejected" && it.reasonCode.isNotBlank()) {
                    Text(reasonLabel(it.reasonCode), style = MaterialTheme.typography.bodySmall, color = palette.danger)
                }
            }
            if (verification?.verified != true && review?.status != "pending") {
                SpacerH(12)
                ActionButton(
                    label = if (busy) "Requesting…" else "Request manual review",
                    price = null,
                    onClick = onRequest,
                    enabled = !busy,
                )
            }
            error?.let {
                SpacerH(8)
                Text(it, style = MaterialTheme.typography.bodySmall, color = palette.danger)
            }
            SpacerH(12)
            Text(
                "Verification needs a live presentation of a government-issued photo ID.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.inkSecondary,
            )
            SpacerH(4)
            Text(
                "Never upload, email or message a copy of your ID. This app does not accept or store documents, images or selfies.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.danger,
            )
        }
    }
}

/** Owner-only review queue. The server remains the authority on every action. */
@Composable
fun AgeReviewScreen(store: AgeStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    var revokeUser by remember { mutableStateOf("") }
    var revokeReason by remember { mutableStateOf(RejectReasons.first().first) }
    var showRevoke by remember { mutableStateOf(false) }

    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { store.loadQueue() }

    Column(Modifier.fillMaxSize().background(palette.ground).imePadding()) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).background(palette.chrome).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier.size(48.dp).pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) {
                Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink)
            }
            Column(Modifier.weight(1f)) {
                Text("Age review", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text("Owner · decisions require MFA", style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
        }
        Divider()
        LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp)) {
            item {
                Text(
                    "Reviewers see a live presentation. Never ask for, accept or keep a copy of an ID.",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.danger,
                )
                SpacerH(12)
                if (store.queueLoading) LabelMono("Loading queue…")
                store.queueError?.let { Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall) }
                if (!store.queueLoading && store.queue.isEmpty() && store.queueError == null) {
                    Text("No reviews are waiting.", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
            }
            items(store.queue, key = { it.id }) { item ->
                SpacerH(10)
                CardSurface {
                    Row(
                        Modifier.fillMaxWidth()
                            .pressScale(motion, onClick = { scope.launch { store.open(item.id) } })
                            .padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("Account " + item.userId.take(8), style = MaterialTheme.typography.titleMedium, color = palette.ink)
                            Text("Requested " + (item.requestedAt?.take(16) ?: "—"), style = MaterialTheme.typography.bodySmall, color = palette.muted)
                        }
                        LabelMono(statusLabel(item.status))
                    }
                }
            }
            item {
                SpacerH(20)
                OutlineButton(label = if (showRevoke) "Hide revoke control" else "Revoke a verification",
                    onClick = { showRevoke = !showRevoke })
                if (showRevoke) {
                    SpacerH(10)
                    CardSurface {
                        Column(Modifier.padding(16.dp)) {
                            Text("Revoke by account UUID", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                            Text("The account loses adult access immediately.", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                            SpacerH(8)
                            Field(value = revokeUser, placeholder = "Account UUID", onChange = { revokeUser = it })
                            SpacerH(8)
                            ReasonPicker(selected = revokeReason, onSelect = { revokeReason = it })
                            SpacerH(10)
                            ActionButton(
                                label = "Revoke",
                                price = null,
                                onClick = {
                                    scope.launch {
                                        if (store.revoke(revokeUser.trim(), revokeReason)) {
                                            showRevoke = false
                                            revokeUser = ""
                                        }
                                    }
                                },
                                enabled = !store.deciding && revokeUser.isNotBlank(),
                            )
                        }
                    }
                }
                store.actionError?.let {
                    SpacerH(10)
                    Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }

    val selected = store.detail
    if (selected != null) {
        ReviewDetailSheet(store = store, request = selected, onClose = { store.clearDetail() })
    }
    if (store.mfaRequired) {
        StepUpSheet(store = store, onDismiss = { store.dismissStepUp() })
    }
}

@Composable
private fun ReviewDetailSheet(
    store: AgeStore,
    request: AgeReviewRequest,
    onClose: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var reason by remember { mutableStateOf(RejectReasons.first().first) }

    androidx.compose.ui.window.Dialog(onDismissRequest = onClose) {
        CardSurface {
            Column(Modifier.padding(18.dp)) {
                Text("Review request", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text(
                    "Account " + request.userId + "\nStatus: " + statusLabel(request.status),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.inkSecondary,
                )
                SpacerH(10)
                Text(
                    "Approve only after a live presentation of a government-issued ID. Do not store or request a copy.",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
                SpacerH(12)
                Text("If rejecting, choose a non-sensitive reason:", style = MaterialTheme.typography.bodySmall, color = palette.ink)
                SpacerH(6)
                ReasonPicker(selected = reason, onSelect = { reason = it })
                SpacerH(12)
                ActionButton(
                    label = if (store.deciding) "Working…" else "Approve",
                    price = null,
                    onClick = { scope.launch { store.approve(request.id) } },
                    enabled = !store.deciding,
                )
                SpacerH(8)
                OutlineButton(label = "Reject with reason", onClick = {
                    scope.launch { store.reject(request.id, reason) }
                })
                store.actionError?.let {
                    SpacerH(8)
                    Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

@Composable
private fun StepUpSheet(store: AgeStore, onDismiss: () -> Unit) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var code by remember { mutableStateOf("") }

    androidx.compose.ui.window.Dialog(onDismissRequest = onDismiss) {
        CardSurface {
            Column(Modifier.padding(18.dp)) {
                Text("Confirm it is you", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text(
                    "Enter a current authenticator code. Review decisions need a fresh check.",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
                SpacerH(10)
                Field(value = code, placeholder = "6-digit code", onChange = { code = it.take(6) })
                SpacerH(10)
                ActionButton(
                    label = if (store.stepUpBusy) "Checking…" else "Unlock review",
                    price = null,
                    onClick = { scope.launch { store.stepUp(code) } },
                    enabled = !store.stepUpBusy && code.length >= 6,
                )
                store.stepUpError?.let {
                    SpacerH(8)
                    Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

@Composable
private fun ReasonPicker(selected: String, onSelect: (String) -> Unit) {
    val palette = LocalVesperPalette.current
    Column {
        RejectReasons.forEach { (code, label) ->
            Row(
                Modifier.fillMaxWidth().pressScale(LocalVesperMotion.current, onClick = { onSelect(code) })
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(if (selected == code) "●" else "○", color = palette.brass)
                Text("  " + label, style = MaterialTheme.typography.bodySmall, color = palette.ink)
            }
        }
    }
}

@Composable
private fun Field(value: String, placeholder: String, onChange: (String) -> Unit) {
    val palette = LocalVesperPalette.current
    Box(
        Modifier.fillMaxWidth().height(46.dp)
            .background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        if (value.isEmpty()) Text(placeholder, style = MaterialTheme.typography.bodySmall, color = palette.muted)
        BasicTextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
            cursorBrush = SolidColor(palette.ink),
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
