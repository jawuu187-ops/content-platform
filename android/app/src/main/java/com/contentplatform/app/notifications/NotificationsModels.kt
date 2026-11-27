package com.contentplatform.app.notifications

data class NotificationItem(
    val id: String,
    val category: String,
    val title: String,
    val body: String,
    val link: String,
    val read: Boolean,
    val createdAt: String,
)

data class NotificationPreferences(
    val inApp: Boolean = true,
    val messages: Boolean = true,
    val engagement: Boolean = true,
    val payments: Boolean = true,
    val email: Boolean = false,
    val push: Boolean = false,
    val marketing: Boolean = false,
)

sealed interface NotificationsState {
    data object Idle : NotificationsState
    data object Loading : NotificationsState
    data class Ready(val items: List<NotificationItem>) : NotificationsState
    data class Failed(val message: String) : NotificationsState
}
