package com.contentplatform.app.age

/**
 * Age-verification state and a manual-review request.
 *
 * These types deliberately have no field for a document, image or date of birth. The
 * server never sends one, and the app has no upload control to produce one.
 */
data class AgeVerification(
    val status: String,
    val verified: Boolean,
    val minAge: Int,
    val expiresAt: String?,
    val checkedAt: String?,
)

data class AgeReviewRequest(
    val id: String,
    val userId: String,
    val status: String,
    val method: String,
    val reasonCode: String,
    val requestedAt: String?,
    val decidedAt: String?,
    val expiresAt: String?,
)

val RejectReasons = listOf(
    "age_not_established" to "Age not established",
    "identity_mismatch" to "Identity does not match",
    "presentation_failed" to "Live presentation failed",
    "suspected_fraud" to "Suspected fraud",
    "other" to "Other review failure",
)

fun reasonLabel(code: String): String =
    RejectReasons.firstOrNull { it.first == code }?.second ?: code.replace('_', ' ')

fun statusLabel(status: String): String = when (status) {
    "unverified" -> "Not verified"
    "pending" -> "Review pending"
    "verified" -> "Verified 18+"
    "expired" -> "Verification expired"
    "revoked" -> "Verification revoked"
    "rejected" -> "Review declined"
    "approved" -> "Approved"
    else -> status.replace('_', ' ')
}
