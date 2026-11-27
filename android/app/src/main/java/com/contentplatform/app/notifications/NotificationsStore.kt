package com.contentplatform.app.notifications

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import java.io.IOException

class NotificationsStore(private val api: NotificationsApi) {
    var state by mutableStateOf<NotificationsState>(NotificationsState.Idle)
        private set
    var unread by mutableStateOf(0)
        private set
    var preferences by mutableStateOf(NotificationPreferences())
        private set
    var busy by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set

    suspend fun refreshCount() {
        runCatching { api.unread() }.onSuccess { unread = it }
    }

    suspend fun load() {
        state = NotificationsState.Loading
        error = null
        try {
            val (items, count) = api.list()
            unread = count
            state = NotificationsState.Ready(items)
            preferences = api.preferences()
        } catch (t: Throwable) {
            state = NotificationsState.Failed(explain(t))
        }
    }

    suspend fun markRead(item: NotificationItem) {
        if (item.read || busy) return
        busy = true
        try {
            unread = api.markRead(item.id)
            val current = state as? NotificationsState.Ready
            if (current != null) {
                state = NotificationsState.Ready(current.items.map {
                    if (it.id == item.id) it.copy(read = true) else it
                })
            }
        } catch (t: Throwable) {
            error = explain(t)
        } finally {
            busy = false
        }
    }

    suspend fun markAllRead() {
        if (busy) return
        busy = true
        try {
            api.markAllRead()
            unread = 0
            val current = state as? NotificationsState.Ready
            if (current != null) {
                state = NotificationsState.Ready(current.items.map { it.copy(read = true) })
            }
        } catch (t: Throwable) {
            error = explain(t)
        } finally {
            busy = false
        }
    }

    suspend fun setInApp(value: Boolean) = save(preferences.copy(inApp = value))
    suspend fun setMessages(value: Boolean) = save(preferences.copy(messages = value))
    suspend fun setEngagement(value: Boolean) = save(preferences.copy(engagement = value))
    suspend fun setPayments(value: Boolean) = save(preferences.copy(payments = value))
    suspend fun setMarketing(value: Boolean) = save(preferences.copy(marketing = value))

    private suspend fun save(next: NotificationPreferences) {
        if (busy) return
        busy = true
        error = null
        try {
            preferences = api.savePreferences(next)
        } catch (t: Throwable) {
            error = explain(t)
        } finally {
            busy = false
        }
    }

    fun clearError() { error = null }

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            401 -> "Your session has expired. Sign in again."
            403 -> t.detail.ifBlank { "Notifications are not available for this account." }
            else -> t.detail.ifBlank { "The server rejected the notification request." }
        }
        is IOException -> "You are offline. The last loaded notifications are shown."
        else -> t.message ?: "The notification request failed."
    }
}
