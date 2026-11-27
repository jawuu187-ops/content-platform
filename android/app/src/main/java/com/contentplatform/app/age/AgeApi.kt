package com.contentplatform.app.age

import com.contentplatform.app.core.SessionRefresh
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

/**
 * HTTP client for age verification and manual review.
 *
 * A request carries only a decision code; there is no multipart path, no file reader and no
 * camera permission in the app. The owner endpoints are still enforced by the server.
 */
class AgeApi(private val sessions: SessionStore) {

    suspend fun verification(): AgeVerification =
        parseVerification(JSONObject(get("/v1/verify/age")).optJSONObject("verification"))

    suspend fun review(): AgeReviewRequest? =
        parseReview(JSONObject(get("/v1/verify/age/review")).optJSONObject("review"))

    suspend fun requestReview(): AgeReviewRequest? =
        parseReview(JSONObject(post("/v1/verify/age/review", "{}")).optJSONObject("review"))

    suspend fun queue(): List<AgeReviewRequest> {
        val arr = JSONObject(get("/v1/owner/verify/reviews")).optJSONArray("reviews")
        val out = mutableListOf<AgeReviewRequest>()
        if (arr != null) {
            for (i in 0 until arr.length()) {
                parseReview(arr.optJSONObject(i))?.let { out += it }
            }
        }
        return out
    }

    suspend fun reviewDetail(id: String): AgeReviewRequest? =
        parseReview(JSONObject(get("/v1/owner/verify/reviews/${encode(id)}")).optJSONObject("review"))

    suspend fun approve(id: String): AgeReviewRequest? =
        parseReview(JSONObject(post("/v1/owner/verify/reviews/${encode(id)}/approve", "{}")).optJSONObject("review"))

    suspend fun reject(id: String, reasonCode: String): AgeReviewRequest? {
        val payload = JSONObject().put("reason_code", reasonCode).toString()
        return parseReview(JSONObject(post("/v1/owner/verify/reviews/${encode(id)}/reject", payload)).optJSONObject("review"))
    }

    suspend fun revoke(userId: String, reasonCode: String): Int {
        val payload = JSONObject().put("reason_code", reasonCode).toString()
        val body = post("/v1/owner/verify/users/${encode(userId)}/revoke", payload)
        return JSONObject(body).optInt("revoked", 0)
    }

    suspend fun stepUp(code: String) {
        val payload = JSONObject().put("code", code.trim()).toString()
        post("/v1/auth/mfa/step-up", payload)
    }

    private fun parseVerification(json: JSONObject?): AgeVerification {
        if (json == null) throw ApiException(502, "invalid_response", "the server returned no verification state")
        return AgeVerification(
            status = json.optString("status"),
            verified = json.optBoolean("verified"),
            minAge = json.optInt("min_age", 18),
            expiresAt = json.optString("expires_at").takeIf { it.isNotBlank() },
            checkedAt = json.optString("checked_at").takeIf { it.isNotBlank() },
        )
    }

    private fun parseReview(json: JSONObject?): AgeReviewRequest? {
        if (json == null) return null
        return AgeReviewRequest(
            id = json.optString("id"),
            userId = json.optString("user_id"),
            status = json.optString("status"),
            method = json.optString("method"),
            reasonCode = json.optString("reason_code"),
            requestedAt = json.optString("requested_at").takeIf { it.isNotBlank() },
            decidedAt = json.optString("decided_at").takeIf { it.isNotBlank() },
            expiresAt = json.optString("expires_at").takeIf { it.isNotBlank() },
        )
    }

    private fun encode(value: String): String = URLEncoder.encode(value, "UTF-8")

    private fun requireSession(): com.contentplatform.app.dm.Session {
        val session = sessions.current()
        if (!session.signedIn) throw ApiException(401, "signed_out", "not signed in")
        return session
    }

    private suspend fun get(path: String): String {
        val session = requireSession()
        return request("GET", session.baseUrl + path, session.accessToken, null)
    }

    private suspend fun post(path: String, payload: String): String {
        val session = requireSession()
        return request("POST", session.baseUrl + path, session.accessToken, payload)
    }

    private suspend fun request(method: String, url: String, token: String, body: String?): String {
        return try {
            execute(method, url, token, body)
        } catch (e: ApiException) {
            if (token.isNotBlank() && e.status == 401 && SessionRefresh.refresh(sessions)) {
                execute(method, url, requireSession().accessToken, body)
            } else {
                throw e
            }
        }
    }

    private suspend fun execute(method: String, url: String, token: String, body: String?): String =
        withContext(Dispatchers.IO) {
            val connection = URL(url).openConnection() as HttpURLConnection
            try {
                connection.requestMethod = method
                connection.connectTimeout = 10_000
                connection.readTimeout = 20_000
                connection.useCaches = false
                connection.setRequestProperty("Accept", "application/json")
                if (token.isNotBlank()) connection.setRequestProperty("Authorization", "Bearer $token")
                if (body != null) {
                    connection.doOutput = true
                    connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                    connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                }
                val status = connection.responseCode
                val stream = if (status in 200..299) connection.inputStream else connection.errorStream
                val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
                if (status !in 200..299) throw problem(status, text)
                text
            } finally {
                connection.disconnect()
            }
        }

    private fun problem(status: Int, body: String): ApiException {
        if (body.isBlank()) return ApiException(status, "", "")
        return try {
            val json = JSONObject(body)
            ApiException(status, json.optString("code"), json.optString("detail"))
        } catch (e: Exception) {
            ApiException(status, "", "")
        }
    }
}
