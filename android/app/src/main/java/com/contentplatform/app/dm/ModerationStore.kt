package com.contentplatform.app.dm

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Safety reporting for everyone, and the review queue for people who hold `safety.review`.
 *
 * The queue is not hidden by guesswork: the app asks `/v1/auth/me` for the permission list
 * and shows the entry only when the server says the account may review. A 403 from the queue
 * endpoint is still rendered honestly, because permissions can change between screens.
 */

sealed interface ReportSubmitState {
    data object Idle : ReportSubmitState
    data object Sending : ReportSubmitState
    data class Done(val report: Report) : ReportSubmitState
    data class Failed(val message: String) : ReportSubmitState
}

sealed interface QueueState {
    data object Idle : QueueState
    data object Loading : QueueState
    data object Denied : QueueState
    data class Ready(val items: List<Report>) : QueueState
    data class Failed(val failure: DmFailure) : QueueState
}

sealed interface DetailState {
    data object Idle : DetailState
    data object Loading : DetailState
    data class Ready(val report: Report) : DetailState
    data class Failed(val message: String) : DetailState
}

class ModerationStore(
    private val sessions: SessionStore,
    private val api: DmApi,
) {
    var permissions by mutableStateOf<List<String>>(emptyList())
        private set
    var roles by mutableStateOf<List<String>>(emptyList())
        private set
    var meLoaded by mutableStateOf(false)
        private set

    var submitState by mutableStateOf<ReportSubmitState>(ReportSubmitState.Idle)
        private set
    var queue by mutableStateOf<QueueState>(QueueState.Idle)
        private set
    var detail by mutableStateOf<DetailState>(DetailState.Idle)
        private set
    var deciding by mutableStateOf(false)
        private set
    var decideError by mutableStateOf<String?>(null)
        private set

    val canModerate: Boolean get() = permissions.contains("safety.review")

    /** Reads the account once per app session; a signed-out account gets empty lists. */
    suspend fun loadMe() {
        if (!sessions.current().signedIn) {
            permissions = emptyList()
            roles = emptyList()
            meLoaded = true
            return
        }
        try {
            val me = withContext(Dispatchers.IO) { api.me() }
            roles = strings(me.optJSONArray("roles"))
            permissions = strings(me.optJSONArray("permissions"))
        } catch (t: Throwable) {
            // A failed lookup must not silently grant or deny anything: it leaves the
            // entry hidden and the queue honest about being unavailable.
            roles = emptyList()
            permissions = emptyList()
        } finally {
            meLoaded = true
        }
    }

    fun startReport() {
        submitState = ReportSubmitState.Idle
    }

    fun clearReport() {
        submitState = ReportSubmitState.Idle
    }

    suspend fun submit(subjectType: String, subjectId: String, reason: String, details: String) {
        if (subjectId.isBlank()) {
            submitState = ReportSubmitState.Failed("There is nothing here to report.")
            return
        }
        if (reason.isBlank()) {
            submitState = ReportSubmitState.Failed("Choose what is wrong.")
            return
        }
        submitState = ReportSubmitState.Sending
        submitState = try {
            val report = api.submitReport(subjectType, subjectId, reason, details)
            if (report == null) {
                ReportSubmitState.Failed("The server did not confirm the report. Try again.")
            } else {
                ReportSubmitState.Done(report)
            }
        } catch (t: Throwable) {
            ReportSubmitState.Failed(t.toDmFailure().explain())
        }
    }

    suspend fun loadQueue(status: String = "") {
        queue = QueueState.Loading
        queue = try {
            val items = api.moderationQueue(status)
            QueueState.Ready(items)
        } catch (t: Throwable) {
            val failure = t.toDmFailure()
            if (failure is DmFailure.NotAllowed) QueueState.Denied else QueueState.Failed(failure)
        }
    }

    suspend fun loadDetail(id: String) {
        detail = DetailState.Loading
        detail = try {
            val report = api.reportDetail(id)
            if (report == null) DetailState.Failed("That report no longer exists.") else DetailState.Ready(report)
        } catch (t: Throwable) {
            DetailState.Failed(t.toDmFailure().explain())
        }
    }

    /** Applies a decision and shows the trail the server returned, not an assumed one. */
    suspend fun decide(id: String, action: String, note: String) {
        if (deciding) return
        decideError = null
        deciding = true
        try {
            val updated = api.transition(id, action, note)
            if (updated == null) {
                decideError = "The server did not confirm the change."
            } else {
                detail = DetailState.Ready(updated)
                // Keep the queue fresh so a closed report does not linger in it.
                if (queue is QueueState.Ready) loadQueue()
            }
        } catch (t: Throwable) {
            decideError = t.toDmFailure().explain()
        } finally {
            deciding = false
        }
    }

    private fun strings(array: org.json.JSONArray?): List<String> {
        if (array == null) return emptyList()
        val out = mutableListOf<String>()
        for (i in 0 until array.length()) {
            val v = array.optString(i)
            if (v.isNotBlank()) out += v
        }
        return out
    }

    companion object {
        fun reasonLabel(code: String): String = ModerationReason.label(code)

        fun actionLabel(action: String): String = when (action) {
            "start_review" -> "Review started"
            "action" -> "Action taken"
            "dismiss" -> "Dismissed"
            "reopen" -> "Reopened"
            else -> action
        }
    }
}
