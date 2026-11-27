package com.contentplatform.app.auth

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/** Account security: email verification, TOTP enrolment and sign-out. */
class SecurityStore(
    private val sessions: SessionStore,
    private val api: AuthApi,
) {
    var email by mutableStateOf("")
        private set
    var emailVerified by mutableStateOf(false)
        private set
    var busy by mutableStateOf(false)
        private set
    var message by mutableStateOf<String?>(null)
        private set
    var totpSecret by mutableStateOf<String?>(null)
        private set
    var totpURI by mutableStateOf<String?>(null)
        private set
    var recoveryCodes by mutableStateOf<List<String>>(emptyList())
        private set

    suspend fun load() {
        busy = true
        message = null
        try {
            val session = sessions.current()
            if (!session.signedIn) return
            val text = withContext(Dispatchers.IO) {
                val connection = URL(session.baseUrl + "/v1/auth/me").openConnection() as HttpURLConnection
                try {
                    connection.requestMethod = "GET"
                    connection.setRequestProperty("Authorization", "Bearer " + session.accessToken)
                    connection.setRequestProperty("Accept", "application/json")
                    if (connection.responseCode !in 200..299) throw ApiException(connection.responseCode, "", "")
                    connection.inputStream.bufferedReader().use { it.readText() }
                } finally {
                    connection.disconnect()
                }
            }
            val body = JSONObject(text)
            email = body.optString("email")
            emailVerified = body.optBoolean("email_verified")
        } catch (t: Throwable) {
            message = explain(t)
        } finally {
            busy = false
        }
    }

    suspend fun requestEmail() {
        action { api.requestEmailVerification(); message = "Verification email sent." }
    }

    suspend fun confirmEmail(code: String) {
        action {
            api.confirmEmail(code)
            emailVerified = true
            message = "Email verified."
        }
    }

    suspend fun startTOTP() {
        action {
            val (secret, uri) = api.startTOTP("Android app")
            totpSecret = secret
            totpURI = uri
            message = "Add the secret to your authenticator, then confirm with a current code."
        }
    }

    suspend fun confirmTOTP(code: String) {
        action {
            recoveryCodes = api.confirmTOTP(code)
            message = "Two-factor authentication enabled. Store the recovery codes now."
        }
    }

    suspend fun logout() {
        action { api.logout() }
    }

    fun clearMessage() {
        message = null
    }

    private suspend fun action(block: suspend () -> Unit) {
        if (busy) return
        busy = true
        message = null
        try {
            block()
        } catch (t: Throwable) {
            message = explain(t)
        } finally {
            busy = false
        }
    }

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            401 -> "Your session has expired. Sign in again."
            409 -> t.detail.ifBlank { "Two-factor authentication is already enabled." }
            else -> t.detail.ifBlank { "The request failed." }
        }
        else -> t.message ?: "The request failed."
    }
}
