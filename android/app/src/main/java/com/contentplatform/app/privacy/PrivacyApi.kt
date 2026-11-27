package com.contentplatform.app.privacy

import com.contentplatform.app.core.SessionRefresh
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

data class Consent(val kind: String, val granted: Boolean)
data class Deletion(val id: String, val status: String, val scheduledFor: String)

class PrivacyApi(private val sessions: SessionStore) {
    suspend fun export(): String = request("GET", "/v1/me/export", null)

    suspend fun consents(): List<Consent> {
        val arr = JSONObject(request("GET", "/v1/me/consents", null)).optJSONArray("consents") ?: JSONArray()
        return (0 until arr.length()).mapNotNull { i ->
            val o = arr.optJSONObject(i) ?: return@mapNotNull null
            Consent(o.optString("kind"), o.isNull("revoked_at"))
        }
    }

    suspend fun setConsent(kind: String, granted: Boolean): List<Consent> {
        val body = JSONObject().put("kind", kind).put("granted", granted).toString().toByteArray()
        val arr = JSONObject(request("PATCH", "/v1/me/consents", body)).optJSONArray("consents") ?: JSONArray()
        return (0 until arr.length()).mapNotNull { i ->
            val o = arr.optJSONObject(i) ?: return@mapNotNull null
            Consent(o.optString("kind"), o.isNull("revoked_at"))
        }
    }

    suspend fun deletion(): Deletion? {
        val o = JSONObject(request("GET", "/v1/me/deletion", null)).optJSONObject("deletion") ?: return null
        return Deletion(o.optString("id"), o.optString("status"), o.optString("scheduled_for"))
    }

    suspend fun requestDeletion(password: String): Deletion {
        val body = JSONObject().put("password", password).put("confirm", "DELETE").toString().toByteArray()
        val o = JSONObject(request("POST", "/v1/me/deletion", body)).optJSONObject("deletion")
            ?: throw ApiException(502, "invalid_response", "deletion missing")
        return Deletion(o.optString("id"), o.optString("status"), o.optString("scheduled_for"))
    }

    suspend fun cancelDeletion(): Deletion {
        val o = JSONObject(request("DELETE", "/v1/me/deletion", null)).optJSONObject("deletion")
            ?: throw ApiException(502, "invalid_response", "deletion missing")
        return Deletion(o.optString("id"), o.optString("status"), o.optString("scheduled_for"))
    }

    private suspend fun request(method: String, path: String, body: ByteArray?): String {
        val s = sessions.current()
        if (!s.signedIn) throw ApiException(401, "signed_out", "not signed in")
        return try {
            execute(method, s.baseUrl + path, s.accessToken, body)
        } catch (e: ApiException) {
            if (e.status == 401 && SessionRefresh.refresh(sessions)) {
                execute(method, s.baseUrl + path, sessions.current().accessToken, body)
            } else throw e
        }
    }

    private suspend fun execute(method: String, url: String, token: String, body: ByteArray?): String =
        withContext(Dispatchers.IO) {
            val c = URL(url).openConnection() as HttpURLConnection
            try {
                c.requestMethod = method
                c.connectTimeout = 10_000
                c.readTimeout = 30_000
                c.useCaches = false
                c.setRequestProperty("Accept", "application/json")
                c.setRequestProperty("Authorization", "Bearer $token")
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
                    throw ApiException(status, o?.optString("code").orEmpty(), o?.optString("detail").orEmpty())
                }
                text
            } finally { c.disconnect() }
        }
}
