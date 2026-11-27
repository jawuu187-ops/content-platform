package com.contentplatform.app.ui.notifications

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.contentplatform.app.notifications.NotificationItem
import com.contentplatform.app.notifications.NotificationsState
import com.contentplatform.app.notifications.NotificationsStore
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Chrome
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SectionTitle
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

@Composable
fun NotificationsScreen(store: NotificationsStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { store.load() }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Notifications", subtitle = "Activity and preferences")
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text("Back", color = palette.muted) }
            Spacer(Modifier.weight(1f))
            if (store.unread > 0) {
                TextButton(onClick = { scope.launch { store.markAllRead() } }, enabled = !store.busy) {
                    Text("Mark all read (${store.unread})", color = palette.actionText)
                }
            }
        }
        store.error?.let {
            Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(16.dp))
        }
        LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp)) {
            when (val state = store.state) {
                NotificationsState.Idle, NotificationsState.Loading ->
                    item { LabelMono("Loading notifications…") }
                is NotificationsState.Failed ->
                    item {
                        CardSurface {
                            Column(Modifier.padding(16.dp)) {
                                Text(state.message, color = palette.danger)
                                SpacerH(8)
                                OutlineButton("Retry", onClick = { scope.launch { store.load() } })
                            }
                        }
                    }
                is NotificationsState.Ready -> {
                    if (state.items.isEmpty()) {
                        item {
                            CardSurface {
                                Column(Modifier.padding(16.dp)) {
                                    Text("Nothing yet", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                                    Text("Memberships, messages, comments and payments appear here.",
                                        color = palette.muted, style = MaterialTheme.typography.bodySmall)
                                }
                            }
                        }
                    }
                    items(state.items, key = { it.id }) { item ->
                        NotificationRow(item, busy = store.busy) {
                            scope.launch { store.markRead(item) }
                        }
                        SpacerH(8)
                    }
                }
            }
            item {
                SpacerH(16)
                SectionTitle("Preferences", "in-app")
                SpacerH(8)
                CardSurface {
                    Column(Modifier.padding(16.dp)) {
                        PrefRow("In-app notifications", store.preferences.inApp) {
                            scope.launch { store.setInApp(it) }
                        }
                        PrefRow("Messages", store.preferences.messages) {
                            scope.launch { store.setMessages(it) }
                        }
                        PrefRow("Likes and comments", store.preferences.engagement) {
                            scope.launch { store.setEngagement(it) }
                        }
                        PrefRow("Payments and memberships", store.preferences.payments) {
                            scope.launch { store.setPayments(it) }
                        }
                        PrefRow("Product updates", store.preferences.marketing) {
                            scope.launch { store.setMarketing(it) }
                        }
                        SpacerH(10)
                        Text(
                            "Email and push delivery are not configured. These preferences control the in-app centre only.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                    }
                }
                SpacerH(24)
            }
        }
    }
}

@Composable
private fun NotificationRow(item: NotificationItem, busy: Boolean, onRead: () -> Unit) {
    val palette = LocalVesperPalette.current
    CardSurface {
        Column(Modifier.fillMaxWidth().padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    if (item.read) item.title else "● " + item.title,
                    style = MaterialTheme.typography.titleMedium,
                    color = if (item.read) palette.inkSecondary else palette.ink,
                )
                Spacer(Modifier.weight(1f))
                LabelMono(item.category)
            }
            SpacerH(4)
            Text(item.body, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
            if (!item.read) {
                TextButton(onClick = onRead, enabled = !busy) {
                    Text("Mark read", color = palette.actionText, style = MaterialTheme.typography.labelSmall)
                }
            }
        }
    }
}

@Composable
private fun PrefRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    val palette = LocalVesperPalette.current
    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f), color = palette.ink)
        Switch(checked = checked, onCheckedChange = onChange)
    }
}
