package com.contentplatform.app.finance

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException

/**
 * State for the creator money surfaces.
 *
 * One store per process, like the messaging and moderation stores. It holds what the server
 * last said and never derives a balance, a total or a status itself.
 */
sealed interface StudioState {
    data object Idle : StudioState
    data object Loading : StudioState
    data class Ready(val snapshot: StudioSnapshot, val year: Statement?) : StudioState
    data class Failed(val failure: FinanceFailure) : StudioState
}

sealed interface StatementState {
    data object Idle : StatementState
    data object Loading : StatementState
    data class Ready(val statement: Statement) : StatementState
    data class Failed(val failure: FinanceFailure) : StatementState
}

sealed interface PayoutDetailState {
    data object Idle : PayoutDetailState
    data object Loading : PayoutDetailState
    data class Ready(val events: List<PayoutEvent>) : PayoutDetailState
    data class Failed(val message: String) : PayoutDetailState
}

/** The three period choices the statement offers; custom takes two dates. */
enum class StatementPeriod { ThisYear, Last30Days, Custom }

class FinanceStore(
    private val sessions: SessionStore,
    private val api: FinanceApi,
) {
    var studio by mutableStateOf<StudioState>(StudioState.Idle)
        private set
    var statement by mutableStateOf<StatementState>(StatementState.Idle)
        private set
    var request by mutableStateOf<PayoutRequestState>(PayoutRequestState.Idle)
        private set
    var detail by mutableStateOf<PayoutDetailState>(PayoutDetailState.Idle)
        private set
    var statementPeriod by mutableStateOf(StatementPeriod.ThisYear)
        private set
    var customFrom by mutableStateOf(today().withDayOfYear(1).toString())
        private set
    var customTo by mutableStateOf(today().toString())
        private set
    var includeSimulated by mutableStateOf(false)
        private set
    var statementCurrency by mutableStateOf("USD")
        private set
    var notice by mutableStateOf<String?>(null)
    var adminQueue by mutableStateOf<AdminQueueState>(AdminQueueState.Idle)
        private set
    var adminBusy by mutableStateOf(false)
        private set
    var adminNotice by mutableStateOf<String?>(null)
        private set
    var payments by mutableStateOf<PaymentsState>(PaymentsState.Idle)
        private set
    var saving by mutableStateOf(false)
        private set

    val signedIn: Boolean get() = sessions.current().signedIn

    suspend fun loadPayments() {
        payments = PaymentsState.Loading
        payments = try {
            PaymentsState.Ready(api.myPayments())
        } catch (t: Throwable) {
            PaymentsState.Failed(t.toFinanceFailure().explain())
        }
    }

    suspend fun loadStudio(currency: String = "USD") {
        studio = StudioState.Loading
        try {
            val (balance, payouts) = api.studio(currency)
            val stats = try {
                api.stats()
            } catch (t: Throwable) {
                // A fan or an account without a creator profile has no audience; the money
                // surfaces still render their refusal rather than a screen of zeros.
                if (t.toFinanceFailure() is FinanceFailure.NotAllowed) null else throw t
            }
            val year = try {
                api.statement(yearStart(), today().toString(), currency, false)
            } catch (t: Throwable) {
                null
            }
            val testMoney = try {
                api.excludedTestMoney()
            } catch (t: Throwable) {
                0L
            }
            studio = StudioState.Ready(StudioSnapshot(stats, balance, payouts, testMoney), year)
        } catch (t: Throwable) {
            studio = StudioState.Failed(t.toFinanceFailure())
        }
    }

    suspend fun requestPayout(amountMinor: Long, currency: String, note: String) {
        request = PayoutRequestState.Submitting
        request = try {
            PayoutRequestState.Done(api.requestPayout(amountMinor, currency, note))
        } catch (t: Throwable) {
            PayoutRequestState.Failed(t.toFinanceFailure())
        }
        if (request is PayoutRequestState.Done) loadStudio(currency)
    }

    fun dismissRequest() {
        request = PayoutRequestState.Idle
    }

    suspend fun cancelPayout(id: String, currency: String = "USD") {
        try {
            api.cancelPayout(id)
            notice = "Request cancelled. Nothing was reserved."
            loadStudio(currency)
        } catch (t: Throwable) {
            notice = t.toFinanceFailure().explain()
        }
    }

    suspend fun loadDetail(id: String) {
        detail = PayoutDetailState.Loading
        detail = try {
            val (_, events) = api.payoutDetail(id)
            PayoutDetailState.Ready(events)
        } catch (t: Throwable) {
            PayoutDetailState.Failed(t.toFinanceFailure().explain())
        }
    }

    fun clearDetail() {
        detail = PayoutDetailState.Idle
    }

    fun choosePeriod(period: StatementPeriod) {
        statementPeriod = period
    }

    fun editCustomFrom(value: String) {
        customFrom = value
    }

    fun editCustomTo(value: String) {
        customTo = value
    }

    fun toggleIncludeSimulated() {
        includeSimulated = !includeSimulated
    }

    fun chooseCurrency(value: String) {
        statementCurrency = value.trim().uppercase()
    }

    /** The dates the current period sends; null when a custom date is not a date. */
    fun periodDates(): Pair<String, String>? = when (statementPeriod) {
        StatementPeriod.ThisYear -> yearStart() to today().toString()
        StatementPeriod.Last30Days -> today().minusDays(29).toString() to today().toString()
        StatementPeriod.Custom -> {
            val from = parseDate(customFrom)
            val to = parseDate(customTo)
            if (from == null || to == null || to.isBefore(from)) null else from.toString() to to.toString()
        }
    }

    /** Loads against the studio currency so the statement never guesses one. */
    suspend fun loadStatementForStudio() {
        statementCurrency = (studio as? StudioState.Ready)?.snapshot?.balance?.currency ?: "USD"
        loadStatement(statementCurrency)
    }

    suspend fun loadAdminQueue() {
        adminQueue = AdminQueueState.Loading
        adminQueue = try {
            AdminQueueState.Ready(api.adminQueue())
        } catch (t: Throwable) {
            AdminQueueState.Failed(adminExplain(t))
        }
    }

    suspend fun adminDecision(id: String, action: String, note: String, reference: String) {
        if (adminBusy) return
        adminBusy = true
        adminNotice = null
        try {
            api.adminDecision(id, action, note, reference)
            adminNotice = when (action) {
                "approve" -> "Payout approved; the amount is reserved in the ledger."
                "reject" -> "Payout rejected."
                "settle" -> "Settlement recorded. A recorded settlement is not proof that money moved."
                else -> "Payout updated."
            }
            loadAdminQueue()
        } catch (t: Throwable) {
            adminNotice = adminExplain(t)
        } finally {
            adminBusy = false
        }
    }

    private fun adminExplain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            401 -> "Your session has expired."
            403 -> "This account does not hold the finance review permission."
            409 -> t.detail.ifBlank { "That payout has already changed." }
            else -> t.detail.ifBlank { "The payout action failed." }
        }
        else -> t.message ?: "The payout action failed."
    }

    suspend fun loadStatement(currency: String) {
        val dates = periodDates()
        if (dates == null) {
            statement = StatementState.Failed(FinanceFailure.Invalid("Enter two dates as YYYY-MM-DD, with the start before the end."))
            return
        }
        statement = StatementState.Loading
        statement = try {
            StatementState.Ready(api.statement(dates.first, dates.second, currency, includeSimulated))
        } catch (t: Throwable) {
            StatementState.Failed(t.toFinanceFailure())
        }
    }

    /**
     * Writes the CSV the server produced, without touching it. The file goes into the app's
     * own external files directory: no storage permission and no claim that it landed in
     * the system Downloads folder.
     */
    suspend fun saveStatement(context: Context, statement: Statement): String? {
        saving = true
        return try {
            val path = withContext(Dispatchers.IO) {
                val base = context.getExternalFilesDir(null) ?: context.filesDir
                val dir = File(base, "statements").apply { mkdirs() }
                val file = File(dir, statement.filename)
                file.writeText(statement.csv, Charsets.UTF_8)
                file.absolutePath
            }
            notice = "Saved $path"
            path
        } catch (t: Throwable) {
            notice = "Could not save the file: ${t.message ?: "unknown error"}"
            null
        } finally {
            saving = false
        }
    }

    fun clearNotice() {
        notice = null
    }

    private fun yearStart(): String = today().withDayOfYear(1).toString()

    companion object {
        fun today(): LocalDate = LocalDate.now(ZoneOffset.UTC)

        fun parseDate(value: String): LocalDate? = try {
            LocalDate.parse(value.trim(), DateTimeFormatter.ISO_LOCAL_DATE)
        } catch (t: DateTimeParseException) {
            null
        }

        /** A stable, readable date for a list row: "27 Nov 2026". */
        fun shortDate(value: String): String {
            val date = runCatching { LocalDate.parse(value.take(10)) }.getOrNull() ?: return value
            return date.format(DateTimeFormatter.ofPattern("d MMM"))
        }

        /** Good enough for presence; the server parses dates authoritatively. */
        fun looksLikeDate(value: String): Boolean = parseDate(value) != null
    }
}
