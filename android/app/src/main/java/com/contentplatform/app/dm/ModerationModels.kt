package com.contentplatform.app.dm

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant

/**
 * Safety reports and the moderation queue, as the server returns them.
 *
 * A report carries its own history. The app never invents a status or an event: it renders
 * what the server holds, because that history is what an appeal is decided on.
 */

data class ReportEvent(
    val id: Long,
    val action: String,
    val fromStatus: String,
    val toStatus: String,
    val note: String,
    val createdAtMillis: Long?,
)

data class Report(
    val id: String,
    val reporterId: String,
    val subjectType: String,
    val subjectId: String,
    val reason: String,
    val details: String,
    val status: String,
    val createdAtMillis: Long?,
    val resolvedAtMillis: Long?,
    val events: List<ReportEvent>,
    val duplicate: Boolean,
) {
    val closed: Boolean get() = status == "actioned" || status == "dismissed"
}

/** The reasons the server accepts, with the words a person reads. */
object ModerationReason {
    val codes = listOf(
        "harassment", "nonconsensual", "minor", "impersonation", "spam", "payment", "other",
    )

    fun label(code: String): String = when (code) {
        "harassment" -> "Harassment or threats"
        "nonconsensual" -> "Shared without consent"
        "minor" -> "Involves a minor"
        "impersonation" -> "Impersonation"
        "spam" -> "Spam or scam"
        "payment" -> "Payment problem"
        else -> "Something else"
    }
}

fun parseReport(obj: JSONObject?): Report? {
    if (obj == null) return null
    val id = obj.optString("id")
    if (id.isBlank()) return null
    val events = mutableListOf<ReportEvent>()
    obj.optJSONArray("events")?.let { array ->
        for (i in 0 until array.length()) {
            val e = array.optJSONObject(i) ?: continue
            events += ReportEvent(
                id = e.optLong("id"),
                action = e.optString("action"),
                fromStatus = e.optString("from_status"),
                toStatus = e.optString("to_status"),
                note = e.optString("note"),
                createdAtMillis = parseInstantOrNull(e.optString("created_at")),
            )
        }
    }
    return Report(
        id = id,
        reporterId = obj.optString("reporter_id"),
        subjectType = obj.optString("subject_type"),
        subjectId = obj.optString("subject_id"),
        reason = obj.optString("reason"),
        details = obj.optString("details"),
        status = obj.optString("status", "open"),
        createdAtMillis = parseInstantOrNull(obj.optString("created_at")),
        resolvedAtMillis = parseInstantOrNull(obj.optString("resolved_at")),
        events = events,
        duplicate = obj.optBoolean("duplicate", false),
    )
}

fun parseReports(array: JSONArray?): List<Report> {
    if (array == null) return emptyList()
    val out = mutableListOf<Report>()
    for (i in 0 until array.length()) {
        parseReport(array.optJSONObject(i))?.let { out += it }
    }
    return out
}

/** Milliseconds since the epoch for an RFC 3339 timestamp, or null. */
fun reportTime(millis: Long?): String = millis?.let {
    Instant.ofEpochMilli(it).atZone(java.time.ZoneId.systemDefault())
        .format(java.time.format.DateTimeFormatter.ofPattern("d MMM HH:mm"))
} ?: ""
