package com.contentplatform.app.notifications

import com.contentplatform.app.core.SessionRefresh
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

class NotificationsApi(private val sessions: SessionStore) {

    suspend fun list(): Pair<List<NotificationItem>, Int> {
        val body = JSONObject(request("GET", "/v1/notifications", null))
        val arr = body.optJSONArray("notifications")
        val items = (0 until (arr?.length() ?: 0)).mapNotNull { i ->
            val o = arr?.optJSONObject(i) ?: return@mapNotNull null
            NotificationItem(
                id = o.optString("id"),
                category = o.optString("category"),
                title = o.optString("title"),
                body = o.optString("body"),
                link = o.optString("link"),
                read = !o.isNull("read_at"),
                createdAt = o.optString("created_at"),
            )
        }
        return items to body.optInt("unread_count")
    }

    suspend fun unread(): Int =
        JSONObject(request("GET", "/v1/notifications/unread-count", null)).optInt("unread_count")

    suspend fun markRead(id: String): Int =
        JSONObject(request("POST", "/v1/notifications/$id/read", "{}".toByteArray())).optInt("unread_count")

    suspend fun markAllRead(): Int {
        request("POST", "/v1/notifications/read-all", "{}".toByteArray())
        return 0
    }

    suspend fun preferences(): NotificationPreferences {
        val o = JSONObject(request("GET", "/v1/notifications/preferences", null))
            .optJSONObject("preferences") ?: return NotificationPreferences()
        return parse(o)
    }

    suspend fun savePreferences(p: NotificationPreferences): NotificationPreferences {
        val payload = JSONObject()
            .put("in_app", p.inApp)
            .put("messages", p.messages)
            .put("engagement", p.engagement)
            .put("payments", p.payments)
            .put("marketing", p.marketing)
            .toString().toByteArray()
        val o = JSONObject(request("PATCH", "/v1/notifications/preferences", payload))
            .optJSONObject("preferences") ?: return p
        return parse(o)
    }

    private fun parse(o: JSONObject) = NotificationPreferences(
        inApp = o.optBoolean("in_app", true),
        messages = o.optBoolean("messages", true),
        engagement = o.optBoolean("engagement", true),
        payments = o.optBoolean("payments", true),
        email = o.optBoolean("email", false),
        push = o.optBoolean("push", false),
        marketing = o.optBoolean("marketing", false),
    )

    private suspend fun request(method: String, path: String, body: ByteArray?): String {
        val s = sessions.current()
        if (!s.signedIn) throw ApiException(401, "signed_out", "not signed in")
        return try {
            execute(method, s.baseUrl + path, s.accessToken, body)
        } catch (e: ApiException) {
            if (e.status == 401 && SessionRefresh.refresh(sessions)) {
                execute(method, s.baseUrl + path, sessions.current().accessToken, body)
            } else {
                throw e
            }
        }
    }

    private suspend fun execute(method: String, url: String, token: String, body: ByteArray?): String =
        withContext(Dispatchers.IO) {
            val c = URL(url).openConnection() as HttpURLConnection
            try {
                c.requestMethod = method
                c.connectTimeout = 10_000
                c.readTimeout = 15_000
                c.useCaches = false
                c.setRequestProperty("Accept", "application/json")
                if (token.isNotBlank()) c.setRequestProperty("Authorization", "Bearer $token")
                if (body != null) {
                    c.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                    c.doOutput = true
                    c.outputStream.use { it.write(body) }
                }
                val status = c.responseCode
                val text = (if (status in 200..299) c.inputStream else c.errorStream)
                    ?.bufferedReader()?.use { it.readText() }.orEmpty()
                if (status !in 200..299) {
                    val o = runCatching { JSONObject(text) }.getOrNull()
                    throw ApiException(
                        status,
                        o?.optString("code").orEmpty(),
                        o?.optString("detail").orEmpty(),
                    )
                }
                text
            } finally {
                c.disconnect()
            }
        }
}
