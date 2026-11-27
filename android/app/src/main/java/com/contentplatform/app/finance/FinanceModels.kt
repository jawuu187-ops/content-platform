package com.contentplatform.app.finance

/**
 * The creator money surfaces: studio audience, payout balance, payout requests and the
 * earnings statement.
 *
 * These models mirror the API exactly. Nothing here computes money the server did not send:
 * the balance, the payout amounts and the statement totals all arrive as integers in minor
 * units with their currency, and [money] only formats them.
 */

/** Audience counts from `GET /v1/creator/stats`. */
data class AudienceStats(
    val members: Int,
    val unlocks7d: Int,
    val topTierName: String?,
    val topTierShareBps: Int?,
    val newMembers30d: Int = 0,
    val cancelled30d: Int = 0,
    val trialMembers: Int = 0,
    val posts: Int = 0,
    val likes: Int = 0,
    val comments: Int = 0,
)

/** `balance` from `GET /v1/creator/payouts`. */
data class PayoutBalance(
    val currency: String,
    val availableMinor: Long,
    val pendingMinor: Long,
    val minimumMinor: Long,
)

/** One payout request. `paid` and `failed` exist in the schema but no endpoint sets them. */
data class Payout(
    val id: String,
    val amountMinor: Long,
    val currency: String,
    val method: String,
    val status: String,
    val note: String,
    val decisionNote: String,
    val createdAt: String,
    val decidedAt: String?,
)

data class PayoutEvent(
    val action: String,
    val fromStatus: String,
    val toStatus: String,
    val note: String,
    val createdAt: String,
)

object PayoutStatus {
    const val Requested = "requested"
    const val Approved = "approved"
    const val Rejected = "rejected"
    const val Cancelled = "cancelled"
}

data class AdminPayout(
    val id: String,
    val creatorId: String,
    val amountMinor: Long,
    val currency: String,
    val method: String,
    val status: String,
    val note: String,
    val decisionNote: String,
    val createdAt: String,
    val paidAt: String?,
)

sealed interface AdminQueueState {
    data object Idle : AdminQueueState
    data object Loading : AdminQueueState
    data class Ready(val items: List<AdminPayout>) : AdminQueueState
    data class Failed(val message: String) : AdminQueueState
}

/** What Studio needs in one pass: audience, balance and the latest requests. */
data class StudioSnapshot(
    val stats: AudienceStats?,
    val balance: PayoutBalance,
    val payouts: List<Payout>,
    /** `net_simulated_minor` from the earnings report: money no payout may touch. */
    val testMoneyMinor: Long,
)

/** One payment intent shown in a fan's receipt history. */
data class FanPayment(
    val id: String,
    val purpose: String,
    val amountMinor: Long,
    val currency: String,
    val status: String,
    val provider: String,
    val simulated: Boolean,
    val createdAt: String,
    val settledAt: String?,
)

sealed interface PaymentsState {
    data object Idle : PaymentsState
    data object Loading : PaymentsState
    data class Ready(val items: List<FanPayment>) : PaymentsState
    data class Failed(val message: String) : PaymentsState
}
data class StatementRow(
    val occurredAt: String,
    val kind: String,
    val purpose: String,
    val reference: String,
    val grossMinor: Long,
    val netMinor: Long,
    val simulated: Boolean,
)

/**
 * A statement read from the CSV endpoint, with the totals taken from the response headers so
 * the screen never totals the rows itself.
 */
data class Statement(
    val currency: String,
    val from: String,
    val to: String,
    val rows: List<StatementRow>,
    val grossMinor: Long,
    val netMinor: Long,
    val excludedSimulated: Int,
    val includeSimulated: Boolean,
    val csv: String,
    val filename: String,
)

/** Where the person is in a request or a statement read. */
sealed interface PayoutRequestState {
    data object Idle : PayoutRequestState
    data object Submitting : PayoutRequestState
    data class Done(val payout: Payout) : PayoutRequestState
    data class Failed(val failure: FinanceFailure) : PayoutRequestState
}

sealed interface FinanceFailure {
    data object Offline : FinanceFailure
    data object SignedOut : FinanceFailure
    data class NotAllowed(val detail: String) : FinanceFailure
    data class Invalid(val detail: String) : FinanceFailure

    /** 409: the balance is too small, or a request in this currency is already open. */
    data class Refused(val code: String, val detail: String) : FinanceFailure
    data object NotFound : FinanceFailure
    data class Server(val detail: String) : FinanceFailure
}

/**
 * One sentence a person can act on. Refusals quote the server's own distinction rather than
 * flattening "not enough money" and "there is already an open request" into one error.
 */
fun FinanceFailure.explain(): String = when (this) {
    FinanceFailure.Offline -> "You are offline. Nothing changed."
    FinanceFailure.SignedOut -> "Your session has expired. Sign in again to continue."
    is FinanceFailure.NotAllowed -> detail.ifBlank { "This account cannot use the creator studio." }
    is FinanceFailure.Invalid -> detail
    is FinanceFailure.Refused -> when (code) {
        "insufficient_balance" -> "That is more than your available balance. Lower the amount."
        "payout_conflict" -> "A payout request in this currency is already open. Wait for the decision or cancel it."
        else -> detail.ifBlank { "The server refused that request." }
    }
    FinanceFailure.NotFound -> "That request could not be found."
    is FinanceFailure.Server -> "The server could not handle that request. Try again in a moment."
}

/** Formats integer minor units for the finance surfaces. */
fun money(minor: Long, currency: String): String {
    val symbol = when (currency) {
        "USD" -> "$"
        "EUR" -> "€"
        "GBP" -> "£"
        else -> currency + " "
    }
    return symbol + java.util.Locale.UK.let { java.lang.String.format(it, "%.2f", minor / 100.0) }
}
fun sharePercent(bps: Int?): String {
    if (bps == null) return "—"
    val whole = bps / 100
    val tenth = (bps % 100) / 10
    return if (tenth == 0) "$whole%" else "$whole.$tenth%"
}
