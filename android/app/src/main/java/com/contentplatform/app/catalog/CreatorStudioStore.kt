package com.contentplatform.app.catalog

import android.content.Context
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

sealed interface CreatorToolsState {
    data object Loading : CreatorToolsState
    data object NoProfile : CreatorToolsState
    data class Ready(
        val profile: CreatorProfile,
        val tiers: List<Tier>,
        val posts: List<CreatorPost> = emptyList(),
    ) : CreatorToolsState
    data class Failed(val message: String) : CreatorToolsState
}

/** Creator onboarding, profile/tier management and publishing. */
class CreatorStudioStore(
    private val context: Context,
    private val api: CatalogApi,
) {
    var state by mutableStateOf<CreatorToolsState>(CreatorToolsState.Loading)
        private set
    var busy by mutableStateOf(false)
        private set
    var message by mutableStateOf<String?>(null)
        private set
    var failure by mutableStateOf<String?>(null)
        private set

    suspend fun load() {
        state = CreatorToolsState.Loading
        state = try {
            val mine = api.myProfile()
            if (mine == null) CreatorToolsState.NoProfile
            else {
                val posts = try {
                    api.myPosts()
                } catch (_: Throwable) {
                    api.posts(mine.first.handle)
                }
                CreatorToolsState.Ready(mine.first, mine.second, posts)
            }
        } catch (t: Throwable) {
            CreatorToolsState.Failed(explain(t))
        }
    }

    suspend fun createProfile(handle: String, name: String, bio: String, category: String, published: Boolean) {
        action {
            api.createProfile(handle, name, bio, category, published)
            load()
        }
    }

    suspend fun saveProfile(name: String, bio: String, category: String, published: Boolean) {
        action {
            api.updateProfile(name, bio, category, published)
            load()
        }
    }

    suspend fun addTier(name: String, description: String, priceMinor: Long, currency: String, periodDays: Int) {
        action {
            api.createTier(name, description, priceMinor, currency, periodDays, emptyList())
            load()
        }
    }

    suspend fun setTierActive(id: String, name: String, description: String, priceMinor: Long,
                              currency: String, periodDays: Int, active: Boolean) {
        action {
            api.updateTier(id, name, description, priceMinor, currency, periodDays, active)
            load()
        }
    }

    suspend fun deactivateTier(id: String) {
        action {
            api.deactivateTier(id)
            load()
        }
    }

    suspend fun editTier(
        id: String,
        name: String,
        description: String,
        priceMinor: Long,
        currency: String,
        periodDays: Int,
        active: Boolean,
    ) {
        action {
            api.updateTier(id, name, description, priceMinor, currency, periodDays, active)
            message = "Tier updated."
            load()
        }
    }

    suspend fun setProfileImage(kind: String, uri: android.net.Uri) {
        busy = true
        failure = null
        try {
            val resolver = context.contentResolver
            val mime = resolver.getType(uri) ?: "image/jpeg"
            if (!mime.startsWith("image/")) throw IllegalArgumentException("Choose an image file")
            val bytes = resolver.openInputStream(uri)?.use { it.readBytes() }
                ?: throw IllegalArgumentException("That image could not be opened")
            val mediaId = api.upload(bytes, mime)
            api.setProfileMedia(kind, mediaId)
            load()
        } catch (t: Throwable) {
            failure = t.message ?: "The image could not be saved."
        } finally {
            busy = false
        }
    }

    suspend fun addStory(uri: android.net.Uri) {
        busy = true
        failure = null
        try {
            val resolver = context.contentResolver
            val mime = resolver.getType(uri) ?: "image/jpeg"
            val bytes = resolver.openInputStream(uri)?.use { it.readBytes() }
                ?: throw IllegalArgumentException("That image could not be opened")
            val mediaId = api.upload(bytes, mime)
            api.createStory(mediaId, "")
            message = "Story published for 24 hours."
        } catch (t: Throwable) {
            failure = t.message ?: "The story could not be saved."
        } finally { busy = false }
    }

    suspend fun deletePost(id: String) {
        action {
            api.deletePost(id)
            message = "Post deleted."
            load()
        }
    }

    suspend fun publish(uri: Uri?, body: String, visibility: String, priceMinor: Long?, currency: String,
                        scheduledAt: String? = null) {
        action {
            val mediaId = uri?.let {
                val stream = context.contentResolver.openInputStream(it)
                    ?: throw ApiException(400, "file_unreadable", "the selected file could not be read")
                val mime = context.contentResolver.getType(it) ?: "application/octet-stream"
                api.uploadStream(stream, mime)
            }
            api.createPost(body, visibility, priceMinor, currency, listOfNotNull(mediaId), scheduledAt)
            message = if (scheduledAt == null) "Post published." else "Post scheduled."
            load()
        }
    }

    private suspend fun action(block: suspend () -> Unit) {
        if (busy) return
        busy = true
        message = null
        try {
            block()
        } catch (t: Throwable) {
            message = explain(t)
        } finally {
            busy = false
        }
    }

    fun clearMessage() {
        message = null
    }

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            401 -> "Your session has expired. Sign in again."
            403 -> t.detail.ifBlank { "This account cannot use creator tools." }
            404 -> "Creator profile not found."
            409 -> t.detail.ifBlank { "That handle or tier already exists." }
            413 -> "That file is too large."
            415 -> "Only JPEG, PNG and MP4 are accepted."
            else -> t.detail.ifBlank { "The request failed." }
        }
        else -> t.message ?: "The request failed."
    }
}
