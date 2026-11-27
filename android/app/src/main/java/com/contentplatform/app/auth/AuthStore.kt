package com.contentplatform.app.auth

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import java.io.IOException

/** Sign-in / registration state. No credentials are kept after the request. */
class AuthStore(
    private val sessions: SessionStore,
    private val api: AuthApi,
) {
    var busy by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var mfaRequired by mutableStateOf(false)
        private set

    suspend fun login(baseUrl: String, email: String, password: String, mfaCode: String) {
        if (busy) return
        busy = true
        error = null
        mfaRequired = false
        try {
            api.login(baseUrl, email, password, mfaCode)
        } catch (t: Throwable) {
            val e = t as? ApiException
            if (e?.code == "mfa_required") {
                mfaRequired = true
                error = "Enter the current code from your authenticator."
            } else {
                error = explain(t)
            }
        } finally {
            busy = false
        }
    }

    suspend fun register(baseUrl: String, email: String, password: String) {
        if (busy) return
        busy = true
        error = null
        try {
            api.register(baseUrl, email, password)
        } catch (t: Throwable) {
            error = explain(t)
        } finally {
            busy = false
        }
    }

    suspend fun logout() {
        busy = true
        try {
            api.logout()
        } catch (_: Throwable) {
            sessions.signOut()
        } finally {
            busy = false
        }
    }

    fun clearError() {
        error = null
    }

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            400 -> t.detail.ifBlank { "Check the email, password and server address." }
            401 -> t.detail.ifBlank { "Email or password is incorrect." }
            409 -> t.detail.ifBlank { "That email is already registered." }
            else -> t.detail.ifBlank { "The server could not complete the request." }
        }
        is IOException -> "You are offline."
        else -> t.message ?: "The request failed."
    }
}
