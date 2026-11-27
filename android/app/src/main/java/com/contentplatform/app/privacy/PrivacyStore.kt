package com.contentplatform.app.privacy

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import java.io.IOException

class PrivacyStore(private val api: PrivacyApi) {
    var consents by mutableStateOf<List<Consent>>(emptyList())
        private set
    var deletion by mutableStateOf<Deletion?>(null)
        private set
    var export by mutableStateOf<String?>(null)
        private set
    var busy by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set

    suspend fun load() {
        busy = true
        error = null
        try {
            consents = api.consents()
            deletion = api.deletion()
        } catch (t: Throwable) {
            error = explain(t)
        } finally { busy = false }
    }

    suspend fun exportData() {
        busy = true
        error = null
        try { export = api.export() } catch (t: Throwable) { error = explain(t) } finally { busy = false }
    }

    suspend fun setConsent(kind: String, granted: Boolean) {
        busy = true
        try { consents = api.setConsent(kind, granted) } catch (t: Throwable) { error = explain(t) } finally { busy = false }
    }

    suspend fun requestDeletion(password: String) {
        busy = true
        error = null
        try { deletion = api.requestDeletion(password) } catch (t: Throwable) { error = explain(t) } finally { busy = false }
    }

    suspend fun cancelDeletion() {
        busy = true
        error = null
        try { deletion = api.cancelDeletion() } catch (t: Throwable) { error = explain(t) } finally { busy = false }
    }

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> t.detail.ifBlank { "The privacy request failed." }
        is IOException -> "You are offline."
        else -> t.message ?: "The privacy request failed."
    }
}
