package com.contentplatform.app.auth

import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.Session
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/** Registration, login and the account-security operations the product actually needs. */
class AuthApi(private val sessions: SessionStore) {

    suspend fun register(baseUrl: String, email: String, password: String): Session {
        val payload = JSONObject()
            .put("email", email.trim())
            .put("password", password)
            .put("device", device())
            .toString()
        return auth(baseUrl, "/v1/auth/register", payload)
    }

    suspend fun login(baseUrl: String, email: String, password: String, mfaCode: String): Session {
        val payload = JSONObject()
            .put("email", email.trim())
            .put("password", password)
            .put("mfa_code", mfaCode.trim())
            .put("device", device())
            .toString()
        return auth(baseUrl, "/v1/auth/login", payload)
    }

    suspend fun requestPasswordReset(baseUrl: String, email: String) {
        val root = SessionStore.normalizeBaseUrl(baseUrl)
        if (root.isBlank()) throw ApiException(400, "invalid_base_url", "Enter the server address.")
        request("POST", root + "/v1/auth/password/reset/request", "",
            JSONObject().put("email", email.trim()).toString())
    }

    suspend fun confirmPasswordReset(baseUrl: String, token: String, newPassword: String) {
        val root = SessionStore.normalizeBaseUrl(baseUrl)
        if (root.isBlank()) throw ApiException(400, "invalid_base_url", "Enter the server address.")
        request("POST", root + "/v1/auth/password/reset/confirm", "",
            JSONObject().put("token", token.trim()).put("new_password", newPassword).toString())
    }

    suspend fun requestEmailVerification() {
        postAuthed("/v1/auth/email/verify/request", "{}")
    }

    suspend fun confirmEmail(code: String) {
        postAuthed("/v1/auth/email/verify/confirm", JSONObject().put("code", code.trim()).toString())
    }

    suspend fun startTOTP(label: String): Pair<String, String> {
        val body = JSONObject(postAuthed("/v1/auth/mfa/totp/start", JSONObject().put("label", label).toString()))
        return body.optString("secret") to body.optString("otpauth_uri")
    }

    suspend fun confirmTOTP(code: String): List<String> {
        val body = JSONObject(postAuthed("/v1/auth/mfa/totp/confirm", JSONObject().put("code", code.trim()).toString()))
        val arr = body.optJSONArray("recovery_codes")
        val out = mutableListOf<String>()
        for (i in 0 until (arr?.length() ?: 0)) arr?.optString(i)?.let(out::add)
        return out
    }

    suspend fun logout() {
        try {
            postAuthed("/v1/auth/logout", "{}")
        } finally {
            sessions.signOut()
        }
    }

    private suspend fun auth(baseUrl: String, path: String, payload: String): Session {
        val root = SessionStore.normalizeBaseUrl(baseUrl)
        if (root.isBlank()) throw ApiException(400, "invalid_base_url", "Enter the server address.")
        val text = request("POST", root + path, "", payload)
        val body = JSONObject(text)
        val tokens = body.optJSONObject("tokens")
            ?: throw ApiException(502, "invalid_response", "sign-in response carried no tokens")
        val access = tokens.optString("access_token")
        val refresh = tokens.optString("refresh_token")
        if (access.isBlank() || refresh.isBlank()) {
            throw ApiException(502, "invalid_response", "sign-in response carried no usable tokens")
        }
        val session = Session(
            baseUrl = root,
            accessToken = access,
            email = body.optJSONObject("user")?.optString("email").orEmpty(),
            refreshToken = refresh,
            accessExpiresAt = tokens.optString("access_expires_at"),
        )
        sessions.save(session)
        return session
    }

    private suspend fun postAuthed(path: String, payload: String): String {
        val s = sessions.current()
        if (!s.signedIn) throw ApiException(401, "signed_out", "not signed in")
        return request("POST", s.baseUrl + path, s.accessToken, payload)
    }

    private fun device(): JSONObject = JSONObject()
        .put("fingerprint", sessions.deviceFingerprint())
        .put("platform", SessionStore.platformName())
        .put("name", SessionStore.deviceName())

    private suspend fun request(method: String, url: String, token: String, payload: String): String =
        withContext(Dispatchers.IO) {
            val connection = URL(url).openConnection() as HttpURLConnection
            try {
                connection.requestMethod = method
                connection.connectTimeout = 10_000
                connection.readTimeout = 20_000
                connection.useCaches = false
                connection.setRequestProperty("Accept", "application/json")
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                if (token.isNotBlank()) connection.setRequestProperty("Authorization", "Bearer $token")
                connection.doOutput = true
                connection.outputStream.use { it.write(payload.toByteArray(Charsets.UTF_8)) }
                val status = connection.responseCode
                val stream = if (status in 200..299) connection.inputStream else connection.errorStream
                val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
                if (status !in 200..299) {
                    val detail = try {
                        JSONObject(text)
                    } catch (e: Exception) {
                        null
                    }
                    throw ApiException(
                        status,
                        detail?.optString("code").orEmpty(),
                        detail?.optString("detail").orEmpty(),
                    )
                }
                text
            } finally {
                connection.disconnect()
            }
        }
}
