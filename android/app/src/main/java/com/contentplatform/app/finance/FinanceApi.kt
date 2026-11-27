package com.contentplatform.app.finance

import com.contentplatform.app.core.SessionRefresh
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.Session
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

/**
 * HTTP for the creator money endpoints.
 *
 * It uses the same session, the same plain `HttpURLConnection` and the same problem-document
 * error shape as the messaging client, so a signed-out or offline state looks identical
 * everywhere in the app. Money stays in integer minor units: this class parses numbers, it
 * never converts them.
 */
class FinanceApi(private val sessions: SessionStore) {

    suspend fun stats(): AudienceStats {
        val body = JSONObject(get("/v1/creator/stats"))
        val stats = body.optJSONObject("stats") ?: throw ApiException(502, "invalid_response", "no stats")
        val tiers = stats.optJSONArray("tiers")
        var topName: String? = null
        var topShare: Int? = null
        if (tiers != null && tiers.length() > 0) {
            val top = tiers.getJSONObject(0)
            topName = top.optString("name")
            topShare = top.optInt("share_bps", 0)
        }
        return AudienceStats(
            members = stats.optInt("members", 0),
            unlocks7d = stats.optInt("unlocks_7d", 0),
            topTierName = topName,
            topTierShareBps = topShare,
            newMembers30d = stats.optInt("new_members_30d", 0),
            cancelled30d = stats.optInt("cancelled_30d", 0),
            trialMembers = stats.optInt("trial_members", 0),
            posts = stats.optInt("posts", 0),
            likes = stats.optInt("likes", 0),
            comments = stats.optInt("comments", 0),
        )
    }

    /** Test money the server excludes from every payout figure. */
    suspend fun excludedTestMoney(): Long {
        val body = JSONObject(get("/v1/creator/earnings"))
        val earnings = body.optJSONObject("earnings") ?: return 0L
        return earnings.optLong("net_simulated_minor", 0L)
    }

    suspend fun studio(currency: String): Pair<PayoutBalance, List<Payout>> {
        val body = JSONObject(get("/v1/creator/payouts?currency=" + encode(currency)))
        val balance = body.optJSONObject("balance")
            ?: throw ApiException(502, "invalid_response", "no balance")
        val payouts = ArrayList<Payout>()
        val array = body.optJSONArray("payouts")
        if (array != null) {
            for (i in 0 until array.length()) payouts += parsePayout(array.getJSONObject(i))
        }
        return parseBalance(balance) to payouts
    }

    suspend fun requestPayout(amountMinor: Long, currency: String, note: String): Payout {
        val payload = JSONObject()
            .put("amount_minor", amountMinor)
            .put("currency", currency)
            .put("note", note.trim())
        val body = JSONObject(post("/v1/creator/payouts", payload.toString()))
        val payout = body.optJSONObject("payout")
            ?: throw ApiException(502, "invalid_response", "no payout")
        return parsePayout(payout)
    }

    suspend fun cancelPayout(id: String): Payout {
        val body = JSONObject(post("/v1/creator/payouts/" + encode(id) + "/cancel", ""))
        val payout = body.optJSONObject("payout")
            ?: throw ApiException(502, "invalid_response", "no payout")
        return parsePayout(payout)
    }

    suspend fun payoutDetail(id: String): Pair<Payout, List<PayoutEvent>> {
        val body = JSONObject(get("/v1/creator/payouts/" + encode(id)))
        val payout = body.optJSONObject("payout")
            ?: throw ApiException(502, "invalid_response", "no payout")
        val events = ArrayList<PayoutEvent>()
        val array = payout.optJSONArray("events")
        if (array != null) {
            for (i in 0 until array.length()) {
                val e = array.getJSONObject(i)
                events += PayoutEvent(
                    action = e.optString("action"),
                    fromStatus = e.optString("from_status"),
                    toStatus = e.optString("to_status"),
                    note = e.optString("note"),
                    createdAt = e.optString("created_at"),
                )
            }
        }
        return parsePayout(payout) to events
    }

    /**
     * The CSV export. Totals come from the `X-Export-*` headers, which the server computes
     * from the same rows, so a screen and the file can never disagree.
     */
    suspend fun myPayments(): List<FanPayment> {
        val body = JSONObject(get("/v1/me/payments"))
        val arr = body.optJSONArray("payments") ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i ->
            val p = arr.optJSONObject(i) ?: return@mapNotNull null
            if (p.optString("id").isBlank()) return@mapNotNull null
            FanPayment(
                id = p.optString("id"),
                purpose = p.optString("purpose"),
                amountMinor = p.optLong("amount_minor"),
                currency = p.optString("currency"),
                status = p.optString("status"),
                provider = p.optString("provider"),
                simulated = p.optBoolean("simulated"),
                createdAt = p.optString("created_at"),
                settledAt = p.optString("settled_at").takeIf { it.isNotBlank() },
            )
        }
    }

    suspend fun adminQueue(): List<AdminPayout> {
        val body = JSONObject(get("/v1/admin/payouts?status=all"))
        val arr = body.optJSONArray("payouts") ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i -> parseAdminPayout(arr.optJSONObject(i)) }
    }

    suspend fun adminDecision(id: String, action: String, note: String, reference: String): AdminPayout {
        val payload = if (action == "settle") {
            JSONObject().put("reference", reference.trim()).toString()
        } else {
            JSONObject().put("note", note.trim()).toString()
        }
        val body = JSONObject(post("/v1/admin/payouts/" + encode(id) + "/" + action, payload))
        return parseAdminPayout(body.optJSONObject("payout"))
            ?: throw ApiException(502, "invalid_response", "payout missing from response")
    }

    private fun parseAdminPayout(json: JSONObject?): AdminPayout? {
        if (json == null || json.optString("id").isBlank()) return null
        return AdminPayout(
            id = json.optString("id"),
            creatorId = json.optString("creator_id"),
            amountMinor = json.optLong("amount_minor"),
            currency = json.optString("currency"),
            method = json.optString("method"),
            status = json.optString("status"),
            note = json.optString("note"),
            decisionNote = json.optString("decision_note"),
            createdAt = json.optString("created_at"),
            paidAt = json.optString("paid_at").takeIf { it.isNotBlank() },
        )
    }

    suspend fun statement(
        from: String,
        to: String,
        currency: String,
        includeSimulated: Boolean,
    ): Statement {
        val query = buildString {
            append("/v1/creator/earnings/export?from=").append(encode(from))
            append("&to=").append(encode(to))
            append("&currency=").append(encode(currency))
            if (includeSimulated) append("&include_simulated=true")
        }
        val response = request("GET", query, "text/csv")
        val rows = ArrayList<StatementRow>()
        response.body.lineSequence().forEachIndexed { index, line ->
            if (index == 0 || line.isBlank()) return@forEachIndexed
            val parts = line.split(",", limit = 8)
            if (parts.size < 8) return@forEachIndexed
            rows += StatementRow(
                occurredAt = parts[0],
                kind = parts[1],
                purpose = parts[2],
                reference = parts[3],
                grossMinor = parts[5].toLongOrNull() ?: 0L,
                netMinor = parts[6].toLongOrNull() ?: 0L,
                simulated = parts[7].trim() == "test",
            )
        }
        val disposition = response.header("content-disposition")
        val filename = Regex("filename=\"([^\"]+)\"").find(disposition)?.groupValues?.get(1)
            ?: "earnings.csv"
        return Statement(
            currency = response.header("x-export-currency").ifBlank { currency },
            from = from,
            to = to,
            rows = rows,
            grossMinor = response.header("x-export-gross-minor").toLongOrNull() ?: 0L,
            netMinor = response.header("x-export-net-minor").toLongOrNull() ?: 0L,
            excludedSimulated = response.header("x-export-excluded-simulated").toIntOrNull() ?: 0,
            includeSimulated = includeSimulated,
            csv = response.body,
            filename = filename,
        )
    }

    // ------------------------------------------------------------------ transfer

    private class FinanceResponse(val body: String, val headers: Map<String, String>) {
        fun header(name: String): String = headers[name.lowercase()].orEmpty()
    }

    private suspend fun get(path: String): String = request("GET", path, "application/json").body

    private suspend fun post(path: String, payload: String): String =
        request("POST", path, "application/json", payload).body

    private suspend fun request(
        method: String,
        path: String,
        accept: String,
        body: String? = null,
    ): FinanceResponse {
        return try {
            execute(method, path, accept, body)
        } catch (e: ApiException) {
            if (e.status == 401 && SessionRefresh.refresh(sessions)) {
                execute(method, path, accept, body)
            } else {
                throw e
            }
        }
    }

    private suspend fun execute(
        method: String,
        path: String,
        accept: String,
        body: String? = null,
    ): FinanceResponse = withContext(Dispatchers.IO) {
        val session = session()
        val connection = URL(session.baseUrl + path).openConnection() as HttpURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = 10_000
            connection.readTimeout = 20_000
            connection.useCaches = false
            connection.setRequestProperty("Accept", accept)
            connection.setRequestProperty("Authorization", "Bearer " + session.accessToken)
            if (body != null && body.isNotEmpty()) {
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
            }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (status !in 200..299) throw problem(status, text)
            val headers = HashMap<String, String>()
            connection.headerFields.forEach { (key, values) ->
                if (key != null && values != null) headers[key.lowercase()] = values.joinToString(", ")
            }
            FinanceResponse(text, headers)
        } finally {
            connection.disconnect()
        }
    }

    private fun session(): Session {
        val session = sessions.current()
        if (!session.signedIn) {
            throw ApiException(401, "signed_out", "Sign in to continue.")
        }
        return session
    }

    private fun problem(status: Int, body: String): ApiException {
        val json = runCatching { JSONObject(body) }.getOrNull()
        return ApiException(
            status = status,
            code = json?.optString("code").orEmpty(),
            detail = json?.optString("detail").orEmpty(),
        )
    }

    private fun encode(value: String): String = URLEncoder.encode(value, "UTF-8")

    private fun parseBalance(balance: JSONObject) = PayoutBalance(
        currency = balance.optString("currency", "USD"),
        availableMinor = balance.optLong("available_minor", 0L),
        pendingMinor = balance.optLong("pending_minor", 0L),
        minimumMinor = balance.optLong("minimum_minor", 0L),
    )

    private fun parsePayout(payout: JSONObject) = Payout(
        id = payout.optString("id"),
        amountMinor = payout.optLong("amount_minor", 0L),
        currency = payout.optString("currency", "USD"),
        method = payout.optString("method", "manual"),
        status = payout.optString("status"),
        note = payout.optString("note"),
        decisionNote = payout.optString("decision_note"),
        createdAt = payout.optString("created_at"),
        decidedAt = payout.optString("decided_at").ifBlank { null },
    )
}

/** Maps the money endpoints' failures, including the two distinct 409 refusals. */
fun Throwable.toFinanceFailure(): FinanceFailure = when (this) {
    is ApiException -> when {
        status == 401 -> FinanceFailure.SignedOut
        status == 403 -> FinanceFailure.NotAllowed(detail)
        status == 404 -> FinanceFailure.NotFound
        status == 409 -> FinanceFailure.Refused(code, detail)
        status == 400 || status == 422 -> FinanceFailure.Invalid(detail)
        status >= 500 -> FinanceFailure.Server(detail)
        else -> FinanceFailure.Server(detail)
    }
    is IOException -> FinanceFailure.Offline
    else -> FinanceFailure.Server(message ?: "unexpected error")
}
