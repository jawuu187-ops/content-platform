package com.contentplatform.app.ui.privacy

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.BasicTextField
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
import androidx.compose.ui.unit.dp
import com.contentplatform.app.privacy.PrivacyStore
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
fun PrivacyScreen(store: PrivacyStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var password by rememberSaveable { mutableStateOf("") }
    var showExport by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { store.load() }
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Privacy & data", subtitle = "Export, consent and deletion")
        TextButton(onClick = onBack) { Text("Back", color = palette.muted) }
        LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
            item {
                SectionTitle("Consent", "versioned")
                SpacerH(8)
                CardSurface {
                    Column(Modifier.padding(16.dp)) {
                        listOf("terms" to "Terms", "privacy" to "Privacy policy",
                            "analytics" to "Product analytics", "marketing" to "Marketing email").forEach { (kind, label) ->
                            Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                                Text(label, Modifier.weight(1f), color = palette.ink)
                                Switch(
                                    checked = store.consents.firstOrNull { it.kind == kind }?.granted == true,
                                    onCheckedChange = { scope.launch { store.setConsent(kind, it) } },
                                )
                            }
                        }
                        SpacerH(6)
                        Text("Marketing email has no provider configured; the consent is recorded only.",
                            style = MaterialTheme.typography.bodySmall, color = palette.muted)
                    }
                }
                SpacerH(16)
                SectionTitle("Your data", "portable JSON")
                SpacerH(8)
                CardSurface {
                    Column(Modifier.padding(16.dp)) {
                        OutlineButton("Export my data", onClick = {
                            showExport = true
                            scope.launch { store.exportData() }
                        })
                        if (showExport && store.export != null) {
                            SpacerH(8)
                            Text(store.export!!.take(4000), style = MaterialTheme.typography.bodySmall,
                                color = palette.inkSecondary)
                            if (store.export!!.length > 4000) Text("Export truncated in this preview; use the API for the full file.",
                                style = MaterialTheme.typography.bodySmall, color = palette.muted)
                        }
                    }
                }
                SpacerH(16)
                SectionTitle("Delete account", "seven-day grace")
                SpacerH(8)
                CardSurface {
                    Column(Modifier.padding(16.dp)) {
                        Text("Ledger and payment history are retained and anonymized. Profile, sessions and personal fields are removed after the grace period.",
                            style = MaterialTheme.typography.bodySmall, color = palette.muted)
                        SpacerH(8)
                        val current = store.deletion
                        if (current != null && current.status == "scheduled") {
                            Text("Scheduled: " + current.scheduledFor, color = palette.brass)
                            SpacerH(8)
                            ActionButton("Cancel deletion", null, onClick = { scope.launch { store.cancelDeletion() } })
                        } else {
                            BasicTextField(
                                value = password,
                                onValueChange = { password = it },
                                singleLine = true,
                                textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                                cursorBrush = SolidColor(palette.ink),
                                modifier = Modifier.fillMaxWidth().background(palette.surfaceRaised,
                                    androidx.compose.foundation.shape.RoundedCornerShape(8.dp)).padding(10.dp),
                            )
                            SpacerH(4)
                            LabelMono("PASSWORD")
                            SpacerH(8)
                            ActionButton("Request deletion", null, onClick = {
                                scope.launch { store.requestDeletion(password); password = "" }
                            }, enabled = password.isNotBlank() && !store.busy)
                        }
                    }
                }
                store.error?.let {
                    SpacerH(8)
                    Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                }
                SpacerH(24)
            }
        }
    }
}
