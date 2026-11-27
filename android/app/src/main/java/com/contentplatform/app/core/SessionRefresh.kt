package com.contentplatform.app.core

import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/**
 * Exchanges the refresh token for a new access token. A single mutex prevents two failing
 * requests from rotating the same refresh token at once, which the server treats as theft.
 */
object SessionRefresh {
    private val mutex = Mutex()

    suspend fun refresh(sessions: SessionStore): Boolean = mutex.withLock {
        val current = sessions.current()
        if (current.baseUrl.isBlank() || current.refreshToken.isBlank()) return false
        withContext(Dispatchers.IO) {
            val connection = try {
                URL(current.baseUrl + "/v1/auth/refresh").openConnection() as HttpURLConnection
            } catch (t: Throwable) {
                return@withContext false
            }
            try {
                connection.requestMethod = "POST"
                connection.connectTimeout = 10_000
                connection.readTimeout = 20_000
                connection.useCaches = false
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                val body = JSONObject().put("refresh_token", current.refreshToken).toString()
                connection.doOutput = true
                connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                if (connection.responseCode !in 200..299) return@withContext false
                val text = connection.inputStream.bufferedReader().use { it.readText() }
                val tokens = JSONObject(text).optJSONObject("tokens") ?: return@withContext false
                val access = tokens.optString("access_token")
                val refresh = tokens.optString("refresh_token")
                val expires = tokens.optString("access_expires_at")
                if (access.isBlank() || refresh.isBlank()) return@withContext false
                sessions.updateTokens(access, refresh, expires)
                true
            } catch (t: Throwable) {
                false
            } finally {
                connection.disconnect()
            }
        }
    }
}
