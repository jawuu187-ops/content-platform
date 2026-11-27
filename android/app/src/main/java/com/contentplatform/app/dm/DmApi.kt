package com.contentplatform.app.dm

import com.contentplatform.app.core.SessionRefresh
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

/**
 * The HTTP client for the messaging and sign-in endpoints.
 *
 * There is no retry loop and no queue here on purpose: a message that fails must surface to
 * the person who wrote it with the text still in the box, not be re-sent behind their back.
 * The only mutation this client performs on its own is storing the access token after login.
 */

class ApiException(
    val status: Int,
    val code: String,
    val detail: String,
) : Exception(detail.ifBlank { "request failed with status $status" })

sealed interface DmFailure {
    /** No usable connection: the request never reached the server. */
    data object Offline : DmFailure

    /** The token is missing or was rejected; the person has to sign in again. */
    data object SignedOut : DmFailure

    /** The membership ended. Reading history stays possible; writing does not. */
    data object SubscriptionRequired : DmFailure

    data object NotFound : DmFailure

    data class NotAllowed(val detail: String) : DmFailure

    data class Invalid(val detail: String) : DmFailure

    data class Server(val detail: String) : DmFailure
}

fun Throwable.toDmFailure(): DmFailure = when (this) {
    is ApiException -> when {
        status == 401 -> DmFailure.SignedOut
        status == 403 && code == "subscription_required" -> DmFailure.SubscriptionRequired
        status == 403 -> DmFailure.NotAllowed(detail.ifBlank { "this conversation is not yours" })
        status == 404 -> DmFailure.NotFound
        status == 422 -> DmFailure.NotAllowed(detail.ifBlank { "that request is not allowed" })
        status == 400 -> DmFailure.Invalid(detail.ifBlank { "the server rejected that message" })
        status >= 500 -> DmFailure.Server(detail.ifBlank { "the server could not handle that request" })
        else -> DmFailure.Server(detail.ifBlank { "request failed" })
    }
    is IOException -> DmFailure.Offline
    else -> DmFailure.Server(message ?: "unexpected error")
}

/** One plain sentence a person can act on. */
fun DmFailure.explain(): String = when (this) {
    DmFailure.Offline -> "You are offline. Nothing was sent — your text is still here."
    DmFailure.SignedOut -> "Your session has expired. Sign in again to continue."
    DmFailure.SubscriptionRequired ->
        "Sending is paused because your membership to this creator has ended. " +
            "Earlier messages stay readable."
    DmFailure.NotFound -> "This conversation no longer exists."
    is DmFailure.NotAllowed -> detail
    is DmFailure.Invalid -> detail
    is DmFailure.Server -> "The server could not be reached properly. Try again in a moment."
}

class DmApi(private val sessions: SessionStore) {

    suspend fun signIn(baseUrl: String, email: String, password: String): Session {
        val root = SessionStore.normalizeBaseUrl(baseUrl)
        if (root.isBlank()) throw ApiException(400, "invalid_base_url", "Enter the server address.")
        if (email.isBlank() || password.isEmpty()) {
            throw ApiException(400, "invalid_credentials", "Enter your email and password.")
        }
        val device = JSONObject()
            .put("fingerprint", sessions.deviceFingerprint())
            .put("platform", SessionStore.platformName())
            .put("name", SessionStore.deviceName())
        val payload = JSONObject()
            .put("email", email.trim())
            .put("password", password)
            .put("mfa_code", "")
            .put("device", device)
            .toString()
        val body = request("POST", "$root/v1/auth/login", "", payload)
        val tokens = JSONObject(body).optJSONObject("tokens")
            ?: throw ApiException(502, "invalid_response", "the sign-in response carried no tokens")
        val token = tokens.optString("access_token")
        val refresh = tokens.optString("refresh_token")
        if (token.isBlank() || refresh.isBlank()) {
            throw ApiException(502, "invalid_response", "the sign-in response carried no usable tokens")
        }
        val session = Session(
            baseUrl = root,
            accessToken = token,
            email = email.trim(),
            refreshToken = refresh,
            accessExpiresAt = tokens.optString("access_expires_at"),
        )
        sessions.save(session)
        return session
    }

    suspend fun conversations(): List<Conversation> =
        parseConversations(get("/v1/dm/conversations"))

    suspend fun thread(conversationId: String): List<Message> =
        parseMessages(get("/v1/dm/conversations/${encode(conversationId)}/messages"))

    suspend fun send(conversationId: String, body: String): Message? {
        val payload = JSONObject().put("body", body).toString()
        return parseSingleMessage(
            post("/v1/dm/conversations/${encode(conversationId)}/messages", payload),
        )
    }

    /**
     * Creates a tip. The amount is the client's to choose here and nowhere else, which is
     * why the server bounds it; the idempotency key makes a retry the same tip rather than
     * a second one.
     */
    suspend fun tip(
        creatorId: String,
        amountMinor: Long,
        note: String,
        idempotencyKey: String,
    ): PaymentIntent? {
        val payload = JSONObject()
            .put("creator_id", creatorId)
            .put("amount_minor", amountMinor)
            .put("note", note.trim())
            .put("idempotency_key", idempotencyKey)
            .toString()
        val body = post("/v1/tips", payload)
        val created = parsePayment(JSONObject(body).optJSONObject("tip")) ?: return null
        val settled = post("/v1/payments/${encode(created.id)}/simulate", "{}")
        return parsePayment(JSONObject(settled).optJSONObject("payment")) ?: created
    }

    /**
     * The account behind the session: roles and permissions decide whether the moderation
     * queue is offered at all. Asking the server beats guessing from the UI.
     */
    suspend fun me(): JSONObject = JSONObject(get("/v1/auth/me"))

    suspend fun submitReport(
        subjectType: String,
        subjectId: String,
        reason: String,
        details: String,
    ): Report? {
        val payload = JSONObject()
            .put("subject_type", subjectType)
            .put("subject_id", subjectId)
            .put("reason", reason)
            .put("details", details.trim())
            .toString()
        return parseReport(JSONObject(post("/v1/reports", payload)).optJSONObject("report"))
    }

    suspend fun moderationQueue(status: String): List<Report> {
        val query = if (status.isBlank()) "" else "?status=" + encode(status)
        return parseReports(JSONObject(get("/v1/moderation/reports$query")).optJSONArray("reports"))
    }

    suspend fun reportDetail(id: String): Report? =
        parseReport(JSONObject(get("/v1/reports/${encode(id)}")).optJSONObject("report"))

    suspend fun transition(id: String, action: String, note: String): Report? {
        val payload = JSONObject().put("action", action).put("note", note.trim()).toString()
        return parseReport(
            JSONObject(post("/v1/moderation/reports/${encode(id)}/transitions", payload))
                .optJSONObject("report"),
        )
    }

    suspend fun open(creatorId: String): Conversation? {
        val payload = JSONObject().put("creator_id", creatorId).toString()
        return parseSingleConversation(post("/v1/dm/conversations", payload))
    }

    suspend fun sendAttachment(conversationId: String, body: String, mediaId: String): Message? {
        val payload = JSONObject().put("body", body).put("media_id", mediaId).toString()
        return parseSingleMessage(
            post("/v1/dm/conversations/${encode(conversationId)}/attachments", payload),
        )
    }

    suspend fun sendPaid(conversationId: String, body: String, priceMinor: Long): Message? {
        val payload = JSONObject().put("body", body).put("price_minor", priceMinor).toString()
        return parseSingleMessage(post("/v1/dm/conversations/${encode(conversationId)}/messages", payload))
    }

    suspend fun request(creatorId: String, body: String, mediaId: String = ""): Conversation? {
        val payload = JSONObject().put("creator_id", creatorId).put("body", body).put("media_id", mediaId).toString()
        return parseSingleConversation(post("/v1/dm/requests", payload))
    }

    suspend fun requests(): List<Conversation> {
        val arr = JSONObject(get("/v1/dm/requests")).optJSONArray("requests") ?: return emptyList()
        return (0 until arr.length()).mapNotNull { arr.optJSONObject(it)?.let(::parseConversation) }
    }

    suspend fun acceptRequest(id: String): Conversation? =
        parseSingleConversation(post("/v1/dm/conversations/${encode(id)}/accept", "{}"))

    suspend fun rejectRequest(id: String): Conversation? =
        parseSingleConversation(post("/v1/dm/conversations/${encode(id)}/reject", "{}"))

    suspend fun blockRequest(id: String): Conversation? =
        parseSingleConversation(post("/v1/dm/conversations/${encode(id)}/block", "{}"))

    suspend fun unlockMessage(messageId: String, key: String): Message? {
        val payload = JSONObject().put("idempotency_key", key).toString()
        return parseSingleMessage(post("/v1/dm/messages/${encode(messageId)}/unlock", payload))
    }

    suspend fun messagingSettings(): JSONObject = JSONObject(get("/v1/dm/settings")).optJSONObject("settings") ?: JSONObject()

    suspend fun saveMessagingSettings(policy: String, paid: Boolean, defaultPrice: Long?, currency: String): JSONObject {
        val payload = JSONObject().put("dm_request_policy", policy)
            .put("paid_messages_enabled", paid).put("message_currency", currency)
        if (defaultPrice != null) payload.put("default_message_price_minor", defaultPrice)
        return JSONObject(patch("/v1/dm/settings", payload.toString())).optJSONObject("settings") ?: JSONObject()
    }

    suspend fun broadcast(body: String): Int {
        val response = post("/v1/dm/broadcast", JSONObject().put("body", body).toString())
        return JSONObject(response).optInt("sent")
    }

    suspend fun messageMedia(messageId: String): ByteArray = withContext(Dispatchers.IO) {
        val s = requireSession()
        val c = URL(s.baseUrl + "/v1/dm/messages/${encode(messageId)}/media").openConnection() as HttpURLConnection
        try {
            c.requestMethod = "GET"
            c.setRequestProperty("Authorization", "Bearer " + s.accessToken)
            val status = c.responseCode
            if (status !in 200..299) throw ApiException(status, "media_unavailable", "attachment could not be loaded")
            c.inputStream.use { it.readBytes() }
        } finally { c.disconnect() }
    }

    suspend fun uploadImage(bytes: ByteArray, contentType: String): String = withContext(Dispatchers.IO) {
        val s = requireSession()
        val c = URL(s.baseUrl + "/v1/media").openConnection() as HttpURLConnection
        try {
            c.requestMethod = "POST"
            c.doOutput = true
            c.setRequestProperty("Authorization", "Bearer " + s.accessToken)
            c.setRequestProperty("Content-Type", contentType)
            c.setRequestProperty("Accept", "application/json")
            c.outputStream.use { it.write(bytes) }
            val status = c.responseCode
            val text = (if (status in 200..299) c.inputStream else c.errorStream)
                ?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (status !in 200..299) throw problem(status, text)
            JSONObject(text).optJSONObject("media")?.optString("id").orEmpty()
                .ifBlank { throw ApiException(502, "invalid_response", "upload returned no media id") }
        } finally { c.disconnect() }
    }

    private fun encode(value: String): String = URLEncoder.encode(value, "UTF-8")

    private fun requireSession(): Session {
        val session = sessions.current()
        if (!session.signedIn) throw ApiException(401, "signed_out", "not signed in")
        return session
    }

    private suspend fun get(path: String): String {
        val session = requireSession()
        return request("GET", session.baseUrl + path, session.accessToken, null)
    }

    private suspend fun post(path: String, payload: String): String {
        val session = requireSession()
        return request("POST", session.baseUrl + path, session.accessToken, payload)
    }

    private suspend fun patch(path: String, payload: String): String {
        val session = requireSession()
        return request("PATCH", session.baseUrl + path, session.accessToken, payload)
    }

    private suspend fun request(
        method: String,
        url: String,
        token: String,
        body: String?,
    ): String {
        return try {
            execute(method, url, token, body)
        } catch (e: ApiException) {
            if (token.isNotBlank() && e.status == 401 && SessionRefresh.refresh(sessions)) {
                execute(method, url, requireSession().accessToken, body)
            } else {
                throw e
            }
        }
    }

    private suspend fun execute(
        method: String,
        url: String,
        token: String,
        body: String?,
    ): String = withContext(Dispatchers.IO) {
        val connection = URL(url).openConnection() as HttpURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = 10_000
            connection.readTimeout = 20_000
            connection.useCaches = false
            connection.setRequestProperty("Accept", "application/json")
            if (token.isNotBlank()) {
                connection.setRequestProperty("Authorization", "Bearer $token")
            }
            if (body != null) {
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
            }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (status !in 200..299) throw problem(status, text)
            text
        } finally {
            connection.disconnect()
        }
    }

    private fun problem(status: Int, body: String): ApiException {
        if (body.isBlank()) return ApiException(status, "", "")
        return try {
            val json = JSONObject(body)
            ApiException(status, json.optString("code"), json.optString("detail"))
        } catch (e: Exception) {
            ApiException(status, "", "")
        }
    }
}
