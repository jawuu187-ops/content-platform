package com.contentplatform.app.catalog

import com.contentplatform.app.core.SessionRefresh
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

/**
 * Creator discovery, profiles, posts, subscriptions and media.
 *
 * Every method maps to an existing backend route. Media bytes are uploaded as the raw
 * request body (the backend sniffs the content); delivery tokens are requested per post
 * and are short-lived.
 */
class CatalogApi(private val sessions: SessionStore) {

    suspend fun simulatedPayments(): Boolean {
        val meta = JSONObject(get("/v1/meta"))
        return meta.optJSONObject("payments")?.optBoolean("simulated") == true
    }

    suspend fun creators(query: String = "", category: String = "", offset: Int = 0): Pair<List<CreatorProfile>, Int> {
        val params = mutableListOf<String>()
        if (query.isNotBlank()) params += "q=" + encode(query)
        if (category.isNotBlank()) params += "category=" + encode(category)
        if (offset > 0) params += "offset=$offset"
        val suffix = if (params.isEmpty()) "" else "?" + params.joinToString("&")
        val body = JSONObject(get("/v1/creators$suffix"))
        val arr = body.optJSONArray("creators")
        val list = (0 until (arr?.length() ?: 0)).mapNotNull { parseCreator(arr?.optJSONObject(it)) }
        return list to body.optInt("next_offset", -1)
    }

    suspend fun creator(handle: String): Pair<CreatorProfile, List<Tier>> {
        val body = JSONObject(get("/v1/creators/" + encode(handle)))
        return parseCreator(body.optJSONObject("profile"))
            .orFail("creator profile missing") to parseTiers(body.optJSONArray("tiers"))
    }

    suspend fun stories(handle: String): List<String> {
        val arr = JSONObject(get("/v1/creators/" + encode(handle) + "/stories")).optJSONArray("stories")
        return (0 until (arr?.length() ?: 0)).mapNotNull { arr?.optJSONObject(it)?.optString("id") }
    }

    suspend fun storyImage(storyId: String): ByteArray {
        val s = requireSession()
        return requestBytes("GET", s.baseUrl + "/v1/stories/" + encode(storyId) + "/media", s.accessToken)
    }

    suspend fun createStory(mediaId: String, caption: String) {
        val payload = JSONObject().put("media_id", mediaId).put("caption", caption.trim()).toString().toByteArray()
        request("POST", requireSession().baseUrl + "/v1/creators/me/stories",
            requireSession().accessToken, payload)
    }

    suspend fun myPosts(): List<CreatorPost> {
        val arr = JSONObject(get("/v1/creators/me/posts")).optJSONArray("posts")
        return (0 until (arr?.length() ?: 0)).mapNotNull { parsePost(arr?.optJSONObject(it)) }
    }

    suspend fun posts(handle: String): List<CreatorPost> {
        val arr = JSONObject(get("/v1/creators/" + encode(handle) + "/posts")).optJSONArray("posts")
        return (0 until (arr?.length() ?: 0)).mapNotNull { parsePost(arr?.optJSONObject(it)) }
    }

    suspend fun subscribe(handle: String, tierId: String, promoCode: String = ""): Subscription? {
        val payload = JSONObject().put("tier_id", tierId).put("promo_code", promoCode.trim()).toString()
        val body = JSONObject(post("/v1/creators/" + encode(handle) + "/subscribe", payload))
        return parseSubscription(body.optJSONObject("subscription"))
    }

    suspend fun cancelSubscription(id: String): Subscription? {
        val body = JSONObject(post("/v1/subscriptions/" + encode(id) + "/cancel", "{}"))
        return parseSubscription(body.optJSONObject("subscription"))
    }

    suspend fun mySubscriptions(): List<Subscription> {
        val arr = JSONObject(get("/v1/me/subscriptions")).optJSONArray("subscriptions")
        return (0 until (arr?.length() ?: 0)).mapNotNull { parseSubscription(arr?.optJSONObject(it)) }
    }

    suspend fun myProfile(): Pair<CreatorProfile, List<Tier>>? {
        val response = getOrNull("/v1/creators/me") ?: return null
        val body = JSONObject(response)
        return parseCreator(body.optJSONObject("profile"))
            ?.let { it to parseTiers(body.optJSONArray("tiers")) }
    }

    suspend fun createProfile(
        handle: String,
        displayName: String,
        bio: String,
        category: String,
        published: Boolean,
    ): CreatorProfile {
        val payload = JSONObject()
            .put("handle", handle.trim())
            .put("display_name", displayName.trim())
            .put("bio", bio.trim())
            .put("category", category)
            .put("published", published)
            .toString()
        return parseCreator(JSONObject(post("/v1/creators", payload)).optJSONObject("profile"))
            .orFail("profile missing from response")
    }

    suspend fun updateProfile(
        displayName: String,
        bio: String,
        category: String,
        published: Boolean,
        welcomeMessage: String = "",
    ): CreatorProfile {
        val payload = JSONObject()
            .put("display_name", displayName.trim())
            .put("bio", bio.trim())
            .put("category", category)
            .put("welcome_message", welcomeMessage.trim())
            .put("published", published)
            .toString()
        return parseCreator(JSONObject(patch("/v1/creators/me", payload)).optJSONObject("profile"))
            .orFail("profile missing from response")
    }

    suspend fun createTier(
        name: String,
        description: String,
        priceMinor: Long,
        currency: String,
        periodDays: Int,
        perks: List<String>,
    ): Tier {
        val payload = JSONObject()
            .put("name", name.trim())
            .put("description", description.trim())
            .put("price_minor", priceMinor)
            .put("currency", currency)
            .put("period_days", periodDays)
            .put("perks", JSONArray(perks))
            .toString()
        return parseTier(JSONObject(post("/v1/creators/me/tiers", payload)).optJSONObject("tier"))
            .orFail("tier missing from response")
    }

    suspend fun updateTier(
        id: String,
        name: String,
        description: String,
        priceMinor: Long,
        currency: String,
        periodDays: Int,
        active: Boolean,
    ): Tier {
        val payload = JSONObject()
            .put("name", name.trim())
            .put("description", description.trim())
            .put("price_minor", priceMinor)
            .put("currency", currency)
            .put("period_days", periodDays)
            .put("active", active)
            .toString()
        val body = JSONObject(patch("/v1/creators/me/tiers/" + encode(id), payload))
        return parseTier(body.optJSONObject("tier")).orFail("tier missing from response")
    }

    suspend fun deactivateTier(id: String) {
        val s = requireSession()
        request("DELETE", s.baseUrl + "/v1/creators/me/tiers/" + encode(id), s.accessToken, null)
    }

    suspend fun setLike(postId: String, liked: Boolean): CreatorPost {
        val s = requireSession()
        val method = if (liked) "POST" else "DELETE"
        val body = JSONObject(request(method, s.baseUrl + "/v1/posts/" + encode(postId) + "/like", s.accessToken, if (liked) "{}".toByteArray() else null))
        return parsePost(body.optJSONObject("post")) ?: throw ApiException(502, "invalid_response", "post missing from response")
    }

    suspend fun setBookmark(postId: String, saved: Boolean) {
        val s = requireSession()
        val method = if (saved) "POST" else "DELETE"
        request(method, s.baseUrl + "/v1/posts/" + encode(postId) + "/bookmark", s.accessToken, if (saved) "{}".toByteArray() else null)
    }

    suspend fun comments(postId: String): List<PostComment> {
        val arr = JSONObject(get("/v1/posts/" + encode(postId) + "/comments")).optJSONArray("comments")
        return (0 until (arr?.length() ?: 0)).mapNotNull { i ->
            val c = arr?.optJSONObject(i) ?: return@mapNotNull null
            PostComment(
                id = c.optString("id"),
                postId = c.optString("post_id"),
                authorName = c.optString("author_name").ifBlank { "Fan" },
                body = c.optString("body"),
                mine = c.optBoolean("mine"),
            )
        }
    }

    suspend fun addComment(postId: String, body: String): PostComment {
        val s = requireSession()
        val response = JSONObject(request("POST", s.baseUrl + "/v1/posts/" + encode(postId) + "/comments",
            s.accessToken, JSONObject().put("body", body.trim()).toString().toByteArray()))
        val c = response.optJSONObject("comment") ?: throw ApiException(502, "invalid_response", "comment missing from response")
        return PostComment(
            id = c.optString("id"), postId = c.optString("post_id"),
            authorName = c.optString("author_name").ifBlank { "Fan" },
            body = c.optString("body"), mine = c.optBoolean("mine"),
        )
    }

    suspend fun deleteComment(commentId: String) {
        val s = requireSession()
        request("DELETE", s.baseUrl + "/v1/comments/" + encode(commentId), s.accessToken, null)
    }

    suspend fun bookmarks(): List<CreatorPost> {
        val arr = JSONObject(get("/v1/me/bookmarks")).optJSONArray("posts")
        return (0 until (arr?.length() ?: 0)).mapNotNull { parsePost(arr?.optJSONObject(it)) }
    }

    suspend fun setProfileMedia(kind: String, mediaId: String?) {
        val s = requireSession()
        if (mediaId == null) {
            request("DELETE", s.baseUrl + "/v1/creators/me/profile-media/" + encode(kind), s.accessToken, null)
            return
        }
        val payload = JSONObject().put("media_id", mediaId).toString().toByteArray()
        request("POST", s.baseUrl + "/v1/creators/me/profile-media/" + encode(kind), s.accessToken, payload)
    }

    suspend fun profileImage(handle: String, kind: String): ByteArray {
        val s = requireSession()
        return requestBytes("GET", s.baseUrl + "/v1/creators/" + encode(handle) + "/" + encode(kind), s.accessToken)
    }

    suspend fun publishPost(id: String) {
        val s = requireSession()
        val payload = JSONObject().put("published", true).toString().toByteArray()
        request("PATCH", s.baseUrl + "/v1/posts/" + encode(id), s.accessToken, payload)
    }

    suspend fun deletePost(id: String) {
        val s = requireSession()
        request("DELETE", s.baseUrl + "/v1/posts/" + encode(id), s.accessToken, null)
    }

    suspend fun uploadStream(input: java.io.InputStream, contentType: String): String = withContext(Dispatchers.IO) {
        val session = requireSession()
        val connection = URL(session.baseUrl + "/v1/media").openConnection() as HttpURLConnection
        try {
            connection.requestMethod = "POST"
            connection.connectTimeout = 15_000
            connection.readTimeout = 120_000
            connection.useCaches = false
            connection.doOutput = true
            connection.setChunkedStreamingMode(64 * 1024)
            connection.setRequestProperty("Content-Type", contentType)
            connection.setRequestProperty("Accept", "application/json")
            connection.setRequestProperty("Authorization", "Bearer " + session.accessToken)
            connection.outputStream.use { out -> input.use { it.copyTo(out) } }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (status !in 200..299) throw problem(status, text)
            JSONObject(text).optJSONObject("media")?.optString("id").orEmpty()
                .ifBlank { throw ApiException(502, "invalid_response", "upload returned no media id") }
        } finally {
            connection.disconnect()
        }
    }

    suspend fun upload(bytes: ByteArray, contentType: String): String {
        val session = requireSession()
        val response = request("POST", session.baseUrl + "/v1/media", session.accessToken, bytes, contentType)
        return JSONObject(response).optJSONObject("media")?.optString("id").orEmpty()
            .ifBlank { throw ApiException(502, "invalid_response", "upload returned no media id") }
    }

    suspend fun createPost(
        body: String,
        visibility: String,
        priceMinor: Long?,
        currency: String,
        mediaIds: List<String>,
        scheduledAt: String? = null,
    ): CreatorPost {
        val payload = JSONObject()
            .put("body", body.trim())
            .put("visibility", visibility)
            .put("currency", currency)
            .put("media_ids", JSONArray(mediaIds))
            .put("published", scheduledAt == null)
        if (scheduledAt != null) payload.put("scheduled_at", scheduledAt)
        if (priceMinor != null) payload.put("price_minor", priceMinor)
        return parsePost(JSONObject(post("/v1/posts", payload.toString())).optJSONObject("post"))
            .orFail("post missing from response")
    }

    suspend fun unlockPost(postId: String) {
        post("/v1/posts/" + encode(postId) + "/unlock", "{}")
    }

    suspend fun issueDelivery(postId: String, mediaId: String): Delivery {
        val payload = JSONObject().put("post_id", postId).toString()
        val body = JSONObject(post("/v1/media/" + encode(mediaId) + "/delivery", payload))
        val delivery = body.optJSONObject("delivery")
            ?: throw ApiException(502, "invalid_response", "delivery response missing")
        var url = delivery.optString("url")
        if (url.startsWith("/")) url = requireSession().baseUrl + url
        return Delivery(url, delivery.optString("expires_at").takeIf { it.isNotBlank() })
    }

    suspend fun mediaContent(postId: String, mediaId: String): ByteArray {
        val s = requireSession()
        return requestBytes(
            "GET",
            s.baseUrl + "/v1/posts/" + encode(postId) + "/media/" + encode(mediaId) + "/content",
            s.accessToken,
        )
    }

    fun videoSource(postId: String, mediaId: String): VideoSource {
        val s = requireSession()
        return VideoSource(
            url = s.baseUrl + "/v1/posts/" + encode(postId) + "/media/" + encode(mediaId) + "/content",
            headers = mapOf("Authorization" to "Bearer " + s.accessToken),
        )
    }

    suspend fun download(url: String): ByteArray = withContext(Dispatchers.IO) {
        val connection = URL(url).openConnection() as HttpURLConnection
        try {
            connection.connectTimeout = 15_000
            connection.readTimeout = 60_000
            connection.useCaches = false
            if (connection.responseCode !in 200..299) {
                throw ApiException(connection.responseCode, "download_failed", "media could not be loaded")
            }
            connection.inputStream.use { it.readBytes() }
        } finally {
            connection.disconnect()
        }
    }

    private fun JSONObject.optString2(key: String): String = optString(key).orEmpty()

    private fun parseCreator(json: JSONObject?): CreatorProfile? {
        if (json == null || json.optString("handle").isBlank()) return null
        return CreatorProfile(
            userId = json.optString2("user_id"),
            handle = json.optString2("handle"),
            displayName = json.optString2("display_name"),
            bio = json.optString2("bio"),
            category = json.optString2("category"),
            welcomeMessage = json.optString("welcome_message"),
            published = json.optBoolean("published"),
        )
    }

    private fun parseTiers(arr: JSONArray?): List<Tier> =
        (0 until (arr?.length() ?: 0)).mapNotNull { parseTier(arr?.optJSONObject(it)) }

    private fun parseTier(json: JSONObject?): Tier? {
        if (json == null || json.optString("id").isBlank()) return null
        val perks = mutableListOf<String>()
        json.optJSONArray("perks")?.let { arr ->
            for (i in 0 until arr.length()) arr.optString(i).takeIf { it.isNotBlank() }?.let(perks::add)
        }
        return Tier(
            id = json.optString2("id"),
            name = json.optString2("name"),
            description = json.optString2("description"),
            priceMinor = json.optLong("price_minor"),
            currency = json.optString("currency"),
            periodDays = json.optInt("period_days", 30),
            trialDays = json.optInt("trial_days", 0),
            perks = perks,
            active = json.optBoolean("active", true),
        )
    }

    private fun parsePost(json: JSONObject?): CreatorPost? {
        if (json == null || json.optString("id").isBlank()) return null
        val media = mutableListOf<PostMedia>()
        json.optJSONArray("media")?.let { arr ->
            for (i in 0 until arr.length()) {
                val m = arr.optJSONObject(i) ?: continue
                media += PostMedia(
                    mediaId = m.optString("media_id"),
                    kind = m.optString("kind"),
                    contentType = m.optString("content_type"),
                    width = m.optInt("width"),
                    height = m.optInt("height"),
                )
            }
        }
        return CreatorPost(
            id = json.optString2("id"),
            body = json.optString2("body"),
            visibility = json.optString2("visibility"),
            priceMinor = if (json.has("price_minor")) json.optLong("price_minor") else null,
            currency = json.optString("currency"),
            status = json.optString2("status"),
            publishedAt = json.optString("published_at").takeIf { it.isNotBlank() },
            locked = json.optBoolean("locked"),
            media = media,
            likes = json.optInt("likes"),
            liked = json.optBoolean("liked"),
            comments = json.optInt("comments"),
            bookmarked = json.optBoolean("bookmarked"),
        )
    }

    private fun parseSubscription(json: JSONObject?): Subscription? {
        if (json == null || json.optString("id").isBlank()) return null
        return Subscription(
            id = json.optString2("id"),
            creatorId = json.optString2("creator_id"),
            tierId = json.optString2("tier_id"),
            status = json.optString2("status"),
            priceMinor = json.optLong("price_minor"),
            currency = json.optString("currency"),
            periodEnd = json.optString("current_period_end").takeIf { it.isNotBlank() },
            cancelAtPeriodEnd = json.optBoolean("cancel_at_period_end"),
        )
    }

    private fun <T> T?.orFail(message: String): T =
        this ?: throw ApiException(502, "invalid_response", message)

    private fun encode(value: String): String = URLEncoder.encode(value, "UTF-8")

    private fun requireSession(): com.contentplatform.app.dm.Session {
        val session = sessions.current()
        if (!session.signedIn) throw ApiException(401, "signed_out", "not signed in")
        return session
    }

    private suspend fun get(path: String): String {
        val s = requireSession()
        return request("GET", s.baseUrl + path, s.accessToken, null)
    }

    private suspend fun getOrNull(path: String): String? = try {
        get(path)
    } catch (e: ApiException) {
        if (e.status == 404) null else throw e
    }

    private suspend fun post(path: String, payload: String): String {
        val s = requireSession()
        return request("POST", s.baseUrl + path, s.accessToken, payload.toByteArray())
    }

    private suspend fun patch(path: String, payload: String): String {
        val s = requireSession()
        return request("PATCH", s.baseUrl + path, s.accessToken, payload.toByteArray())
    }

    private suspend fun requestBytes(method: String, url: String, token: String): ByteArray =
        withContext(Dispatchers.IO) {
            val connection = URL(url).openConnection() as HttpURLConnection
            try {
                connection.requestMethod = method
                connection.connectTimeout = 15_000
                connection.readTimeout = 60_000
                connection.useCaches = false
                connection.setRequestProperty("Authorization", "Bearer $token")
                if (connection.responseCode !in 200..299) {
                    throw ApiException(connection.responseCode, "media_failed", "media could not be loaded")
                }
                connection.inputStream.use { it.readBytes() }
            } finally {
                connection.disconnect()
            }
        }

    private suspend fun request(
        method: String,
        url: String,
        token: String,
        body: ByteArray?,
        contentType: String = "application/json; charset=utf-8",
    ): String {
        return try {
            execute(method, url, token, body, contentType)
        } catch (e: ApiException) {
            if (e.status == 401 && SessionRefresh.refresh(sessions)) {
                execute(method, url, requireSession().accessToken, body, contentType)
            } else {
                throw e
            }
        }
    }

    private suspend fun execute(
        method: String,
        url: String,
        token: String,
        body: ByteArray?,
        contentType: String = "application/json; charset=utf-8",
    ): String = withContext(Dispatchers.IO) {
        val connection = URL(url).openConnection() as HttpURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = 15_000
            connection.readTimeout = 60_000
            connection.useCaches = false
            connection.setRequestProperty("Accept", "application/json")
            if (token.isNotBlank()) connection.setRequestProperty("Authorization", "Bearer $token")
            if (body != null) {
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", contentType)
                connection.outputStream.use { it.write(body) }
            }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (status !in 200..299) throw problem(status, text)
            text
        } catch (e: IOException) {
            throw e
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
