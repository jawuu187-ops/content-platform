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
    /** Non-null after registration until the person verifies or postpones. */
    var pendingVerificationEmail by mutableStateOf<String?>(null)
        private set
    var verificationBusy by mutableStateOf(false)
        private set
    var verificationNotice by mutableStateOf<String?>(null)
        private set
    var verificationError by mutableStateOf<String?>(null)
        private set
    var resetBusy by mutableStateOf(false)
        private set
    var resetNotice by mutableStateOf<String?>(null)
        private set
    var resetError by mutableStateOf<String?>(null)
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
        var created = false
        try {
            api.register(baseUrl, email, password)
            created = true
            pendingVerificationEmail = email.trim()
        } catch (t: Throwable) {
            error = explain(t)
        } finally {
            busy = false
        }
        // A created account stays usable when the mail provider is down; the step can retry.
        if (created) sendVerificationEmail()
    }

    suspend fun sendVerificationEmail() {
        val target = pendingVerificationEmail ?: return
        if (verificationBusy) return
        verificationBusy = true
        verificationError = null
        verificationNotice = null
        try {
            api.requestEmailVerification()
            verificationNotice = "Confirmation code sent to $target. It expires in 30 minutes."
        } catch (t: Throwable) {
            verificationError = explain(t)
        } finally {
            verificationBusy = false
        }
    }

    /** Returns true only when the server accepted the code. */
    suspend fun confirmVerification(code: String): Boolean {
        if (verificationBusy) return false
        verificationBusy = true
        verificationError = null
        try {
            api.confirmEmail(code)
            pendingVerificationEmail = null
            verificationNotice = null
            return true
        } catch (t: Throwable) {
            verificationError = explain(t)
            return false
        } finally {
            verificationBusy = false
        }
    }

    fun postponeVerification() {
        pendingVerificationEmail = null
        verificationNotice = null
        verificationError = null
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

    suspend fun requestPasswordReset(baseUrl: String, email: String): Boolean {
        if (resetBusy) return false
        resetBusy = true
        resetError = null
        resetNotice = null
        return try {
            api.requestPasswordReset(baseUrl, email)
            resetNotice = "If that address has an account, a reset code is waiting in the development mailbox."
            true
        } catch (t: Throwable) {
            resetError = explain(t)
            false
        } finally {
            resetBusy = false
        }
    }

    suspend fun confirmPasswordReset(baseUrl: String, token: String, newPassword: String): Boolean {
        if (resetBusy) return false
        resetBusy = true
        resetError = null
        resetNotice = null
        return try {
            api.confirmPasswordReset(baseUrl, token, newPassword)
            resetNotice = "Password changed. Every device was signed out; sign in again."
            true
        } catch (t: Throwable) {
            resetError = explain(t)
            false
        } finally {
            resetBusy = false
        }
    }

    fun clearReset() {
        resetNotice = null
        resetError = null
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
