package com.contentplatform.app.catalog

data class CreatorProfile(
    val userId: String,
    val handle: String,
    val displayName: String,
    val bio: String,
    val category: String,
    val published: Boolean,
)

data class Tier(
    val id: String,
    val name: String,
    val description: String,
    val priceMinor: Long,
    val currency: String,
    val periodDays: Int,
    val perks: List<String>,
    val active: Boolean,
)

data class PostMedia(
    val mediaId: String,
    val kind: String,
    val contentType: String,
    val width: Int,
    val height: Int,
)

data class CreatorPost(
    val id: String,
    val body: String,
    val visibility: String,
    val priceMinor: Long?,
    val currency: String,
    val status: String,
    val publishedAt: String?,
    val locked: Boolean,
    val media: List<PostMedia>,
)

data class Subscription(
    val id: String,
    val creatorId: String,
    val tierId: String,
    val status: String,
    val priceMinor: Long,
    val currency: String,
    val periodEnd: String?,
    val cancelAtPeriodEnd: Boolean,
)

data class Delivery(val url: String, val expiresAt: String?)

data class VideoSource(val url: String, val headers: Map<String, String>)

/** Formats integer minor units for catalog surfaces. */
fun money(minor: Long, currency: String): String {
    val symbol = when (currency) {
        "USD" -> "$"
        "EUR" -> "€"
        "GBP" -> "£"
        else -> currency + " "
    }
    return symbol + java.util.Locale.UK.let { java.lang.String.format(it, "%.2f", minor / 100.0) }
}
