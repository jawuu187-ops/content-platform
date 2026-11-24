package com.contentplatform.app.dm

import java.math.BigDecimal
import java.math.RoundingMode
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * Presentation rules that are pure functions on purpose: they are the part of the
 * messaging UI that can be unit tested without an emulator, a network or a database.
 *
 * No wording here implies secure messaging. The platform stores message bodies in its
 * own database and there is no end-to-end encryption in this build, so the UI says so
 * in as many words ([NO_E2EE_NOTE]) instead of leaving a gap a reader would fill in.
 */
object DmFormat {

    /** Matches messaging.MaxBodyLength; the server refuses anything longer. */
    const val MaxLength = 4000

    const val NO_E2EE_NOTE =
        "Messages are stored on our servers so they survive reinstalls and can be " +
            "reviewed for reports. This build does not use end-to-end encryption."

    private val clock = DateTimeFormatter.ofPattern("HH:mm", Locale.UK)
    private val dayMonth = DateTimeFormatter.ofPattern("d MMM", Locale.UK)
    private val full = DateTimeFormatter.ofPattern("d MMM yyyy", Locale.UK)

    /** Inbox timestamps: "now", "4m", "3h", "2d", then a date. */
    fun relativeTime(nowMillis: Long, thenMillis: Long): String {
        val delta = nowMillis - thenMillis
        if (delta < 0) return "now"
        val minutes = delta / 60_000
        return when {
            minutes < 1 -> "now"
            minutes < 60 -> "${minutes}m"
            minutes < 60 * 24 -> "${minutes / 60}h"
            minutes < 60 * 24 * 7 -> "${minutes / (60 * 24)}d"
            else -> {
                val zone = ZoneId.systemDefault()
                dayMonth.format(Instant.ofEpochMilli(thenMillis).atZone(zone))
            }
        }
    }

    /** Thread timestamps carry the clock time, and the date once a day has passed. */
    fun threadTime(nowMillis: Long, thenMillis: Long): String {
        val zone = ZoneId.systemDefault()
        val moment = Instant.ofEpochMilli(thenMillis).atZone(zone)
        val sameDay = Instant.ofEpochMilli(nowMillis).atZone(zone).toLocalDate() == moment.toLocalDate()
        return if (sameDay) clock.format(moment) else full.format(moment)
    }

    /** Two initials at most, because a 40 dp monogram cannot hold more. */
    fun initials(name: String): String {
        val words = name.trim().split(' ', '\t', '\n').filter { it.isNotBlank() }
        return when {
            words.isEmpty() -> "?"
            words.size == 1 -> words[0].take(1).uppercase(Locale.UK)
            else -> (words[0].take(1) + words[1].take(1)).uppercase(Locale.UK)
        }
    }

    /** Trims exactly the way the server does, so the client refuses what the server would. */
    fun normalize(raw: String): String = raw.trim()

    fun length(raw: String): Int = raw.trim().length

    fun canSend(raw: String): Boolean {
        val text = normalize(raw)
        return text.isNotEmpty() && text.length <= MaxLength
    }
}

/** Why a draft cannot be sent, or [Ok]. */
sealed interface DraftCheck {
    data object Ok : DraftCheck
    data object Empty : DraftCheck
    data object TooLong : DraftCheck
}

/** Money a person reads, from minor units. */
fun money(amountMinor: Long, currency: String = "USD"): String {
    val negative = amountMinor < 0
    val absolute = if (negative) -amountMinor else amountMinor
    val body = String.format(Locale.UK, "%d.%02d", absolute / 100, absolute % 100)
    val symbol = when (currency.uppercase(Locale.UK)) {
        "USD" -> "$"
        "EUR" -> "\u20ac"
        "GBP" -> "\u00a3"
        else -> ""
    }
    val sign = if (negative) "-" else ""
    return if (symbol.isEmpty()) {
        sign + body + " " + currency.uppercase(Locale.UK)
    } else {
        sign + symbol + body
    }
}

/** Tip bounds, mirrored from the server so a rejected amount is caught before the request. */
const val MinTipMinor = 100L
const val MaxTipMinor = 500_000L
const val MaxTipNote = 280

sealed interface TipCheck {
    data object Ok : TipCheck
    data object BelowMinimum : TipCheck
    data object AboveMaximum : TipCheck
}

fun checkTip(amountMinor: Long): TipCheck = when {
    amountMinor < MinTipMinor -> TipCheck.BelowMinimum
    amountMinor > MaxTipMinor -> TipCheck.AboveMaximum
    else -> TipCheck.Ok
}

/** Parses what a person typed in an amount field. Null means "not money". */
fun parseAmountToMinor(raw: String): Long? {
    val cleaned = raw.trim().removePrefix("$").replace(",", "").replace(" ", "")
    if (cleaned.isEmpty()) return null
    val value = cleaned.toBigDecimalOrNull() ?: return null
    if (value < BigDecimal.ZERO) return null
    return value.movePointRight(2).setScale(0, RoundingMode.DOWN).toLong()
}

fun checkDraft(raw: String): DraftCheck {
    val text = raw.trim()
    return when {
        text.isEmpty() -> DraftCheck.Empty
        text.length > DmFormat.MaxLength -> DraftCheck.TooLong
        else -> DraftCheck.Ok
    }
}
