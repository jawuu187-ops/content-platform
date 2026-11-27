package com.contentplatform.app.age

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import java.io.IOException

/**
 * Age verification and owner review state.
 *
 * The owner queue is only a convenience: every decision is re-authorized on the server with
 * the owner role, `verify.review` and a recent MFA proof. A 403 is surfaced, never hidden.
 */
class AgeStore(
    private val sessions: SessionStore,
    private val api: AgeApi,
) {
    var verification by mutableStateOf<AgeVerification?>(null)
        private set
    var review by mutableStateOf<AgeReviewRequest?>(null)
        private set
    var loading by mutableStateOf(false)
        private set
    var requesting by mutableStateOf(false)
        private set
    var requestError by mutableStateOf<String?>(null)
        private set

    var queue by mutableStateOf<List<AgeReviewRequest>>(emptyList())
        private set
    var queueLoading by mutableStateOf(false)
        private set
    var queueError by mutableStateOf<String?>(null)
        private set
    var detail by mutableStateOf<AgeReviewRequest?>(null)
        private set
    var deciding by mutableStateOf(false)
        private set
    var actionError by mutableStateOf<String?>(null)
        private set

    var mfaRequired by mutableStateOf(false)
        private set
    var stepUpBusy by mutableStateOf(false)
        private set
    var stepUpError by mutableStateOf<String?>(null)
        private set

    suspend fun loadAccount() {
        if (!sessions.current().signedIn) {
            verification = null
            review = null
            return
        }
        loading = true
        requestError = null
        var failure: String? = null
        try {
            verification = api.verification()
        } catch (t: Throwable) {
            // A failed read must not leave a stale "Verified" badge on screen.
            verification = null
            failure = explainFailure(t)
        }
        try {
            review = api.review()
        } catch (t: Throwable) {
            review = null
            if (failure == null) failure = explainFailure(t)
        }
        requestError = failure
        loading = false
    }

    suspend fun requestManual() {
        if (requesting) return
        requestError = null
        requesting = true
        try {
            review = api.requestReview()
            // The request already succeeded; a status refresh failure must not turn it
            // into a retry that the server would reject as already pending.
            try {
                verification = api.verification()
            } catch (_: Throwable) {
            }
        } catch (t: Throwable) {
            requestError = explainFailure(t)
        } finally {
            requesting = false
        }
    }

    suspend fun loadQueue() {
        queueLoading = true
        queueError = null
        try {
            queue = api.queue()
            mfaRequired = false
        } catch (t: Throwable) {
            if (isMFA(t)) mfaRequired = true
            // Never leave a queue from an earlier session on screen after a failed
            // authorization. The backend is still the boundary, but stale rows should
            // not look actionable.
            queue = emptyList()
            detail = null
            queueError = explainFailure(t)
        } finally {
            queueLoading = false
        }
    }

    suspend fun open(id: String) {
        actionError = null
        detail = try {
            api.reviewDetail(id)
        } catch (t: Throwable) {
            if (isMFA(t)) mfaRequired = true
            actionError = explainFailure(t)
            null
        }
    }

    fun clearDetail() {
        detail = null
        actionError = null
    }

    suspend fun approve(id: String) = decide {
        api.approve(id)
    }

    suspend fun reject(id: String, reasonCode: String) = decide {
        api.reject(id, reasonCode)
    }

    private suspend fun decide(block: suspend () -> AgeReviewRequest?) {
        if (deciding) return
        actionError = null
        deciding = true
        try {
            detail = block()
            mfaRequired = false
            // The decision is final at this point. A queue refresh failure is shown as a
            // refresh problem, not as a failed decision that invites a duplicate retry.
            try {
                queue = api.queue()
            } catch (_: Throwable) {
                queueError = "Decision saved. Refresh the queue to see the change."
            }
        } catch (t: Throwable) {
            if (isMFA(t)) mfaRequired = true
            actionError = explainFailure(t)
        } finally {
            deciding = false
        }
    }

    suspend fun revoke(userId: String, reasonCode: String): Boolean {
        if (deciding) return false
        actionError = null
        deciding = true
        return try {
            api.revoke(userId, reasonCode)
            try {
                queue = api.queue()
            } catch (_: Throwable) {
                queueError = "Revocation saved. Refresh the queue to see the change."
            }
            true
        } catch (t: Throwable) {
            if (isMFA(t)) mfaRequired = true
            actionError = explainFailure(t)
            false
        } finally {
            deciding = false
        }
    }

    suspend fun stepUp(code: String) {
        if (stepUpBusy) return
        stepUpError = null
        stepUpBusy = true
        try {
            api.stepUp(code)
            mfaRequired = false
            loadQueue()
        } catch (t: Throwable) {
            stepUpError = explainFailure(t)
        } finally {
            stepUpBusy = false
        }
    }

    fun dismissStepUp() {
        mfaRequired = false
        stepUpError = null
    }

    fun clearActionError() {
        actionError = null
    }

    private fun isMFA(t: Throwable): Boolean =
        t is ApiException && t.code == "mfa_required"

    private fun explainFailure(t: Throwable): String = when {
        isMFA(t) -> "Confirm your authenticator code to continue."
        t is ApiException -> when {
            t.status == 401 -> "Your session has expired. Sign in again to continue."
            t.status == 403 -> t.detail.ifBlank { "This account is not allowed to review age verification." }
            t.status == 404 -> "That review request no longer exists."
            t.status == 409 -> t.detail.ifBlank { "That review is already decided." }
            t.status == 400 -> t.detail.ifBlank { "The server rejected that request." }
            t.status >= 500 -> "The server could not complete that request. Try again in a moment."
            else -> t.detail.ifBlank { "The request failed." }
        }
        t is IOException -> "You are offline. Nothing was sent."
        else -> t.message ?: "The request failed."
    }
}
