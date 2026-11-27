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

    suspend fun loadCreators(query: String = "") {
        creators = CreatorsState.Loading
        creators = try {
            simulatedPayments = api.simulatedPayments()
            CreatorsState.Ready(api.creators(query))
        } catch (t: Throwable) {
            CreatorsState.Failed(explain(t))
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

    fun dismissResult() {
        subscribeState = SubscribeState.Idle
    }

    suspend fun imageBytes(postId: String, mediaId: String): ByteArray =
        api.mediaContent(postId, mediaId)

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
