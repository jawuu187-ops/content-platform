package com.contentplatform.app.ui.kyc

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.contentplatform.app.kyc.CreatorVerification
import com.contentplatform.app.kyc.KycStore
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Chrome
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SectionTitle
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

@Composable
fun CreatorVerificationScreen(
    store: KycStore,
    owner: Boolean,
    onBack: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var name by rememberSaveable { mutableStateOf("") }
    var country by rememberSaveable { mutableStateOf("") }
    var dob by rememberSaveable { mutableStateOf("") }
    var attested by rememberSaveable { mutableStateOf(false) }

    LaunchedEffect(owner) { store.load(owner) }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Creator verification", subtitle = "Identity and eligibility")
        TextButton(onClick = onBack) { Text("Back", color = palette.muted) }
        LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
            item {
                CardSurface {
                    Column(Modifier.padding(16.dp)) {
                        LabelMono("PRIVACY")
                        Text(
                            "This review is metadata-only. Never upload or send an ID image, selfie or document. " +
                                "The owner reviews identity live and records only the decision.",
                            color = palette.inkSecondary,
                            style = MaterialTheme.typography.bodySmall,
                        )
                        SpacerH(10)
                        val current = store.state.verification
                        if (current != null) {
                            Text("Status: " + current.status.uppercase(), color = palette.brass)
                            if (current.reasonCode.isNotBlank()) Text("Reason: " + current.reasonCode, color = palette.danger)
                        } else {
                            Text("No application yet.", color = palette.muted)
                        }
                    }
                }
                SpacerH(12)
            }
            if (store.state.verification?.status != "approved") {
                item {
                    SectionTitle("Application", "18+")
                    SpacerH(8)
                    CardSurface {
                        Column(Modifier.padding(16.dp)) {
                            KycField("Legal name", name) { name = it }
                            SpacerH(8)
                            KycField("Country (2 letters)", country) { country = it.uppercase().take(2) }
                            SpacerH(8)
                            KycField("Date of birth (YYYY-MM-DD)", dob) { dob = it.take(10) }
                            SpacerH(10)
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Switch(checked = attested, onCheckedChange = { attested = it })
                                Text(
                                    " I confirm I am the person named and at least 18.",
                                    color = palette.inkSecondary,
                                    style = MaterialTheme.typography.bodySmall,
                                )
                            }
                            store.state.error?.let {
                                SpacerH(8)
                                Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                            }
                            store.state.notice?.let {
                                SpacerH(8)
                                Text(it, color = palette.actionText, style = MaterialTheme.typography.bodySmall)
                            }
                            SpacerH(10)
                            ActionButton(
                                label = if (store.state.submitting) "Submitting…" else "Submit for review",
                                price = null,
                                onClick = { scope.launch { store.submit(name, country, dob) } },
                                enabled = !store.state.submitting && name.isNotBlank() &&
                                    country.length == 2 && dob.length == 10 && attested,
                            )
                        }
                    }
                    SpacerH(16)
                }
            }
            if (owner) {
                item {
                    SectionTitle("Owner review queue", "recent MFA required")
                    SpacerH(8)
                }
                items(store.state.queue, key = { it.id }) { item ->
                    OwnerVerificationRow(
                        item = item,
                        busy = store.state.submitting,
                        onApprove = { scope.launch { store.approve(item.id) } },
                        onReject = { scope.launch { store.reject(item.id) } },
                    )
                    SpacerH(8)
                }
                item { SpacerH(24) }
            }
        }
    }
}

@Composable
private fun OwnerVerificationRow(item: CreatorVerification, busy: Boolean, onApprove: () -> Unit, onReject: () -> Unit) {
    val palette = LocalVesperPalette.current
    CardSurface {
        Column(Modifier.padding(14.dp)) {
            Text(item.legalName, style = MaterialTheme.typography.titleMedium, color = palette.ink)
            Text(item.countryCode + " · born " + item.dateOfBirth, color = palette.muted,
                style = MaterialTheme.typography.bodySmall)
            SpacerH(6)
            Row {
                TextButton(onClick = onApprove, enabled = !busy) { Text("Approve", color = palette.actionText) }
                TextButton(onClick = onReject, enabled = !busy) { Text("Reject", color = palette.danger) }
            }
        }
    }
}

@Composable
private fun KycField(label: String, value: String, onChange: (String) -> Unit) {
    val palette = LocalVesperPalette.current
    Column {
        LabelMono(label)
        SpacerH(4)
        Box(
            Modifier.fillMaxWidth().background(palette.surfaceRaised,
                androidx.compose.foundation.shape.RoundedCornerShape(8.dp)).padding(10.dp),
        ) {
            BasicTextField(
                value = value,
                onValueChange = onChange,
                singleLine = true,
                textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                cursorBrush = SolidColor(palette.ink),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Text),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
