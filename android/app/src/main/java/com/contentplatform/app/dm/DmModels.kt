package com.contentplatform.app.dm

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.time.OffsetDateTime
import java.time.format.DateTimeParseException

/**
 * The wire types for direct messages.
 *
 * These mirror the JSON the Go handlers emit, nothing more. Parsing lives here so a
 * malformed field degrades to a sane default instead of crashing a thread: a missing
 * timestamp must never take the screen down.
 */

data class Conversation(
    val id: String,
    val creatorId: String,
    val fanId: String,
    val peerName: String,
    val lastMessageBody: String,
    val lastMessageAtMillis: Long?,
    val unreadCount: Int,
    val canReply: Boolean,
)

data class Message(
    val id: String,
    val body: String,
    val sentAtMillis: Long,
    val mine: Boolean,
    val read: Boolean,
)

internal fun parseInstantOrNull(value: String?): Long? {
    if (value.isNullOrBlank()) return null
    return try {
        OffsetDateTime.parse(value).toInstant().toEpochMilli()
    } catch (e: DateTimeParseException) {
        try {
            Instant.parse(value).toEpochMilli()
        } catch (e2: DateTimeParseException) {
            null
        }
    }
}

fun parseConversation(json: JSONObject): Conversation = Conversation(
    id = json.optString("id"),
    creatorId = json.optString("creator_id"),
    fanId = json.optString("fan_id"),
    peerName = json.optString("peer_name").ifBlank { "Member" },
    lastMessageBody = json.optString("last_message_body"),
    lastMessageAtMillis = parseInstantOrNull(json.optString("last_message_at")),
    unreadCount = json.optInt("unread_count", 0),
    canReply = json.optBoolean("can_reply", false),
)

fun parseMessage(json: JSONObject): Message = Message(
    id = json.optString("id"),
    body = json.optString("body"),
    sentAtMillis = parseInstantOrNull(json.optString("sent_at")) ?: 0L,
    mine = json.optBoolean("mine", false),
    read = parseInstantOrNull(json.optString("read_at")) != null,
)

fun parseConversations(body: String): List<Conversation> {
    val root = JSONObject(body)
    val array = root.optJSONArray("conversations") ?: JSONArray()
    return (0 until array.length()).mapNotNull { index ->
        array.optJSONObject(index)?.let(::parseConversation)
    }
}

fun parseMessages(body: String): List<Message> {
    val root = JSONObject(body)
    val array = root.optJSONArray("messages") ?: JSONArray()
    return (0 until array.length()).mapNotNull { index ->
        array.optJSONObject(index)?.let(::parseMessage)
    }
}

fun parseSingleMessage(body: String): Message? =
    JSONObject(body).optJSONObject("message")?.let(::parseMessage)

fun parseSingleConversation(body: String): Conversation? =
    JSONObject(body).optJSONObject("conversation")?.let(::parseConversation)
