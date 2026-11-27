package com.contentplatform.app.kyc

import com.contentplatform.app.core.SessionRefresh
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

class KycApi(private val sessions: SessionStore) {

    suspend fun latest(): CreatorVerification? {
        val body = JSONObject(request("GET", "/v1/creators/me/verification", null))
        return parse(body.optJSONObject("verification"))
    }

    suspend fun submit(name: String, country: String, dob: String): CreatorVerification {
        val payload = JSONObject()
            .put("legal_name", name.trim())
            .put("country_code", country.trim().uppercase())
            .put("date_of_birth", dob.trim())
            .put("attestation", true)
            .toString().toByteArray()
        val body = JSONObject(request("POST", "/v1/creators/me/verification", payload))
        return parse(body.optJSONObject("verification"))
            ?: throw ApiException(502, "invalid_response", "verification missing from response")
    }

    suspend fun queue(): List<CreatorVerification> {
        val arr = JSONObject(request("GET", "/v1/owner/creator-verifications", null))
            .optJSONArray("verifications")
        return (0 until (arr?.length() ?: 0)).mapNotNull { i ->
            parse(arr?.optJSONObject(i))
        }
    }

    suspend fun approve(id: String) {
        request("POST", "/v1/owner/creator-verifications/$id/approve", "{}".toByteArray())
    }

    suspend fun reject(id: String, reason: String) {
        val payload = JSONObject().put("reason", reason).toString().toByteArray()
        request("POST", "/v1/owner/creator-verifications/$id/reject", payload)
    }

    private fun parse(o: JSONObject?): CreatorVerification? {
        if (o == null || o.optString("id").isBlank()) return null
        return CreatorVerification(
            id = o.optString("id"),
            status = o.optString("status"),
            legalName = o.optString("legal_name"),
            countryCode = o.optString("country_code"),
            dateOfBirth = o.optString("date_of_birth"),
            reasonCode = o.optString("reason_code"),
        )
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
                    throw ApiException(status, o?.optString("code").orEmpty(), o?.optString("detail").orEmpty())
                }
                text
            } finally { c.disconnect() }
        }
}
