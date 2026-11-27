package com.contentplatform.app.kyc

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import java.io.IOException

class KycStore(private val api: KycApi) {
    var state by mutableStateOf(KycState())
        private set

    suspend fun load(owner: Boolean) {
        state = state.copy(loading = true, error = null, notice = null)
        try {
            val latest = api.latest()
            val queue = if (owner) api.queue() else emptyList()
            state = state.copy(loading = false, verification = latest, queue = queue)
        } catch (t: Throwable) {
            state = state.copy(loading = false, error = explain(t))
        }
    }

    suspend fun submit(name: String, country: String, dob: String) {
        if (state.submitting) return
        state = state.copy(submitting = true, error = null, notice = null)
        try {
            val v = api.submit(name, country, dob)
            state = state.copy(submitting = false, verification = v,
                notice = "Submitted for owner review. No ID document was uploaded or stored.")
        } catch (t: Throwable) {
            state = state.copy(submitting = false, error = explain(t))
        }
    }

    suspend fun approve(id: String) = decide(id) { api.approve(id) }
    suspend fun reject(id: String) = decide(id) { api.reject(id, "incomplete_presentation") }

    private suspend fun decide(id: String, action: suspend () -> Unit) {
        if (state.submitting) return
        state = state.copy(submitting = true, error = null)
        try {
            action()
            state = state.copy(submitting = false, queue = api.queue())
        } catch (t: Throwable) {
            state = state.copy(submitting = false, error = explain(t))
        }
    }

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            403 -> t.detail.ifBlank { "A recent MFA step-up is required." }
            404 -> "Create a creator profile before applying for verification."
            else -> t.detail.ifBlank { "The verification request failed." }
        }
        is IOException -> "You are offline."
        else -> t.message ?: "The verification request failed."
    }
}
