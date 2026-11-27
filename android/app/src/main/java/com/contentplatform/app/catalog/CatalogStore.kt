package com.contentplatform.app.catalog

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.contentplatform.app.dm.ApiException
import com.contentplatform.app.dm.SessionStore
import java.io.IOException

sealed interface CreatorsState {
    data object Loading : CreatorsState
    data class Ready(val items: List<CreatorProfile>) : CreatorsState
    data class Failed(val message: String) : CreatorsState
}

sealed interface CreatorState {
    data object Loading : CreatorState
    data class Ready(
        val profile: CreatorProfile,
        val tiers: List<Tier>,
        val posts: List<CreatorPost>,
    ) : CreatorState
    data class Failed(val message: String) : CreatorState
}

sealed interface MembershipsState {
    data object Loading : MembershipsState
    data class Ready(val items: List<Subscription>) : MembershipsState
    data class Failed(val message: String) : MembershipsState
}

sealed interface SubscribeState {
    data object Idle : SubscribeState
    data object Working : SubscribeState
    data class Active(val subscription: Subscription) : SubscribeState
    data class PaymentRequired(val notice: String) : SubscribeState
    data class Failed(val message: String) : SubscribeState
}

/** Drives discovery, public creator pages and the subscription request. */
class CatalogStore(
    private val sessions: SessionStore,
    private val api: CatalogApi,
) {
    var creators by mutableStateOf<CreatorsState>(CreatorsState.Loading)
        private set
    var creator by mutableStateOf<CreatorState>(CreatorState.Loading)
        private set
    var subscribeState by mutableStateOf<SubscribeState>(SubscribeState.Idle)
        private set
    var memberships by mutableStateOf<MembershipsState>(MembershipsState.Loading)
        private set
    var openHandle by mutableStateOf<String?>(null)
        private set
    var simulatedPayments by mutableStateOf(false)
        private set
    var unlockingPostId by mutableStateOf<String?>(null)
        private set
    var unlockNotice by mutableStateOf<String?>(null)
        private set
    var unlockError by mutableStateOf<String?>(null)
        private set
    var nextCreatorOffset by mutableStateOf(-1)
        private set
    var creatorQuery by mutableStateOf("")
        private set
    var creatorCategory by mutableStateOf("")
        private set
    var loadingMoreCreators by mutableStateOf(false)
        private set
    var engagementPostId by mutableStateOf<String?>(null)
        private set
    var comments by mutableStateOf<List<PostComment>>(emptyList())
        private set
    var engagementBusy by mutableStateOf(false)
        private set
    var engagementError by mutableStateOf<String?>(null)
        private set

    suspend fun loadCreators(query: String = "", category: String = "") {
        creatorQuery = query
        creatorCategory = category
        creators = CreatorsState.Loading
        creators = try {
            simulatedPayments = api.simulatedPayments()
            val (items, next) = api.creators(query, category)
            nextCreatorOffset = next
            CreatorsState.Ready(items)
        } catch (t: Throwable) {
            nextCreatorOffset = -1
            CreatorsState.Failed(explain(t))
        }
    }

    suspend fun loadMoreCreators() {
        if (nextCreatorOffset < 0 || loadingMoreCreators) return
        val current = creators as? CreatorsState.Ready ?: return
        loadingMoreCreators = true
        try {
            val (items, next) = api.creators(creatorQuery, creatorCategory, nextCreatorOffset)
            nextCreatorOffset = next
            creators = CreatorsState.Ready(current.items + items.filter { new ->
                current.items.none { it.userId == new.userId }
            })
        } catch (_: Throwable) {
            // Keep the current page; the next tap retries the same offset.
        } finally {
            loadingMoreCreators = false
        }
    }

    suspend fun loadMemberships() {
        memberships = MembershipsState.Loading
        memberships = try {
            MembershipsState.Ready(api.mySubscriptions())
        } catch (t: Throwable) {
            MembershipsState.Failed(explain(t))
        }
    }

    suspend fun cancel(subscriptionId: String) {
        memberships = try {
            api.cancelSubscription(subscriptionId)
            MembershipsState.Ready(api.mySubscriptions())
        } catch (t: Throwable) {
            MembershipsState.Failed(explain(t))
        }
    }

    suspend fun open(handle: String) {
        openHandle = handle
        creator = CreatorState.Loading
        subscribeState = SubscribeState.Idle
        unlockNotice = null
        unlockError = null
        creator = try {
            val (profile, tiers) = api.creator(handle)
            val posts = api.posts(handle)
            CreatorState.Ready(profile, tiers, posts)
        } catch (t: Throwable) {
            CreatorState.Failed(explain(t))
        }
    }

    fun close() {
        openHandle = null
        creator = CreatorState.Loading
        subscribeState = SubscribeState.Idle
        unlockNotice = null
        unlockError = null
        unlockingPostId = null
    }

    suspend fun subscribe(tier: Tier) {
        val handle = openHandle ?: return
        subscribeState = SubscribeState.Working
        subscribeState = try {
            val sub = api.subscribe(handle, tier.id)
            when (sub?.status) {
                "active", "pending" -> {
                    if (sub.status == "active") SubscribeState.Active(sub)
                    else SubscribeState.PaymentRequired(
                        "The membership is pending until a payment provider confirms it. No card was charged.",
                    )
                }
                null -> SubscribeState.Failed("The server did not return a subscription.")
                else -> SubscribeState.Failed("Subscription status: " + sub.status)
            }
        } catch (t: Throwable) {
            val e = t as? ApiException
            when {
                e?.status == 402 || e?.code == "payment_required" -> SubscribeState.PaymentRequired(
                    e.detail.ifBlank { "A payment method is required to activate this membership." },
                )
                else -> SubscribeState.Failed(explain(t))
            }
        }
    }

    suspend fun unlock(post: CreatorPost) {
        if (unlockingPostId != null) return
        unlockingPostId = post.id
        unlockNotice = null
        unlockError = null
        try {
            api.unlockPost(post.id)
            val current = creator as? CreatorState.Ready
            if (current != null) {
                creator = current.copy(
                    posts = current.posts.map { if (it.id == post.id) it.copy(locked = false) else it },
                )
            }
            unlockNotice = if (simulatedPayments) {
                "Test unlock granted. No card was charged; this shortcut does not credit creator earnings."
            } else {
                "Post unlocked."
            }
        } catch (t: Throwable) {
            val e = t as? ApiException
            unlockError = if (e?.status == 402 || e?.code == "payment_required") {
                "A real payment provider is required before this post can be unlocked."
            } else {
                explain(t)
            }
        } finally {
            unlockingPostId = null
        }
    }

    suspend fun toggleLike(post: CreatorPost) {
        if (engagementBusy) return
        engagementBusy = true
        engagementError = null
        try {
            val updated = api.setLike(post.id, !post.liked)
            replacePost(updated)
        } catch (t: Throwable) {
            engagementError = explain(t)
        } finally {
            engagementBusy = false
        }
    }

    suspend fun toggleBookmark(post: CreatorPost) {
        if (engagementBusy) return
        engagementBusy = true
        engagementError = null
        try {
            api.setBookmark(post.id, !post.bookmarked)
            replacePost(post.copy(bookmarked = !post.bookmarked))
        } catch (t: Throwable) {
            engagementError = explain(t)
        } finally {
            engagementBusy = false
        }
    }

    suspend fun openComments(post: CreatorPost) {
        if (engagementPostId == post.id) {
            closeComments()
            return
        }
        engagementPostId = post.id
        comments = emptyList()
        engagementError = null
        engagementBusy = true
        try {
            comments = api.comments(post.id)
        } catch (t: Throwable) {
            engagementError = explain(t)
        } finally {
            engagementBusy = false
        }
    }

    suspend fun addComment(post: CreatorPost, body: String) {
        if (engagementBusy || body.isBlank()) return
        engagementBusy = true
        engagementError = null
        try {
            val added = api.addComment(post.id, body)
            comments = comments + added
            replacePost(post.copy(comments = post.comments + 1))
        } catch (t: Throwable) {
            engagementError = explain(t)
        } finally {
            engagementBusy = false
        }
    }

    suspend fun deleteComment(comment: PostComment) {
        if (engagementBusy) return
        engagementBusy = true
        engagementError = null
        try {
            api.deleteComment(comment.id)
            comments = comments.filterNot { it.id == comment.id }
        } catch (t: Throwable) {
            engagementError = explain(t)
        } finally {
            engagementBusy = false
        }
    }

    fun closeComments() {
        engagementPostId = null
        comments = emptyList()
        engagementError = null
    }

    suspend fun loadBookmarks(): List<CreatorPost> {
        return try {
            api.bookmarks()
        } catch (t: Throwable) {
            engagementError = explain(t)
            emptyList()
        }
    }

    private fun replacePost(post: CreatorPost) {
        val current = creator as? CreatorState.Ready ?: return
        creator = current.copy(posts = current.posts.map { if (it.id == post.id) post else it })
    }

    fun clearUnlockMessage() {
        unlockNotice = null
        unlockError = null
    }

    fun dismissResult() {
        subscribeState = SubscribeState.Idle
    }

    suspend fun imageBytes(postId: String, mediaId: String): ByteArray =
        api.mediaContent(postId, mediaId)

    suspend fun profileImageBytes(handle: String, kind: String): ByteArray =
        api.profileImage(handle, kind)

    fun videoSource(postId: String, mediaId: String): VideoSource =
        api.videoSource(postId, mediaId)

    private fun explain(t: Throwable): String = when (t) {
        is ApiException -> when (t.status) {
            401 -> "Your session has expired. Sign in again."
            403 -> t.detail.ifBlank { "This action is not allowed." }
            404 -> "That creator page is not available."
            409 -> t.detail.ifBlank { "That membership already exists." }
            402 -> t.detail.ifBlank { "A payment method is required." }
            else -> t.detail.ifBlank { "The server rejected the request." }
        }
        is IOException -> "You are offline."
        else -> t.message ?: "The request failed."
    }
}
