package com.contentplatform.app.dm

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import java.util.UUID

/**
 * The inbox and thread state machine.
 *
 * One object per app session, created in composition and driven from a composition scope, so
 * every state change happens on the main thread and a rotation does not leave an orphaned
 * request writing into a dead screen. The draft is kept in the store rather than in the text
 * field so a failed send keeps the words exactly where the person left them.
 */

sealed interface InboxState {
    data object Loading : InboxState
    data object Empty : InboxState
    data object SignedOut : InboxState
    data class Ready(val items: List<Conversation>) : InboxState
    data class Failed(val failure: DmFailure) : InboxState
}

sealed interface ThreadState {
    data object Loading : ThreadState
    data class Ready(
        val peerName: String,
        val messages: List<Message>,
        val canReply: Boolean,
    ) : ThreadState
    data class Failed(val failure: DmFailure) : ThreadState
}

/** The tip sheet's own state, so a failed tip never disturbs the message thread. */
sealed interface TipState {
    data object Idle : TipState
    data object Sending : TipState
    data class Done(val payment: PaymentIntent) : TipState
    data class Failed(val message: String) : TipState
}

class DmStore(
    private val sessions: SessionStore,
    private val api: DmApi,
) {

    var session by mutableStateOf(sessions.current())
        private set

    var inbox by mutableStateOf<InboxState>(
        if (session.signedIn) InboxState.Loading else InboxState.SignedOut,
    )
        private set

    var thread by mutableStateOf<ThreadState>(ThreadState.Loading)
        private set

    var draft by mutableStateOf("")
    var sending by mutableStateOf(false)
        private set

    var sendFailure by mutableStateOf<DmFailure?>(null)
        private set

    var signingIn by mutableStateOf(false)
        private set

    var signInError by mutableStateOf<String?>(null)
        private set

    private var openConversationId: String? = null

    /** The conversation currently on screen, with the creator id a tip needs. */
    var openConversation by mutableStateOf<Conversation?>(null)
        private set

    var tipState by mutableStateOf<TipState>(TipState.Idle)
        private set

    private var tipKey: String = ""

    val signedIn: Boolean get() = session.signedIn

    suspend fun refreshInbox() {
        if (!session.signedIn) {
            inbox = InboxState.SignedOut
            return
        }
        inbox = InboxState.Loading
        inbox = try {
            val items = api.conversations()
            if (items.isEmpty()) InboxState.Empty else InboxState.Ready(items)
        } catch (t: Throwable) {
            if (t.toDmFailure() is DmFailure.SignedOut) {
                sessions.signOut()
                session = sessions.current()
                InboxState.SignedOut
            } else {
                InboxState.Failed(t.toDmFailure())
            }
        }
    }

    suspend fun loadThread(conversationId: String) {
        openConversationId = conversationId
        openConversation = (inbox as? InboxState.Ready)?.items?.firstOrNull { it.id == conversationId }
        val known = peerFor(conversationId)
        thread = ThreadState.Loading
        thread = try {
            val messages = api.thread(conversationId)
            ThreadState.Ready(
                peerName = known.name,
                messages = messages,
                canReply = known.canReply && !expired,
            )
        } catch (t: Throwable) {
            ThreadState.Failed(t.toDmFailure())
        }
    }

    suspend fun reloadThread() {
        openConversationId?.let { loadThread(it) }
    }

    suspend fun send(): DmFailure? {
        val conversationId = openConversationId ?: return DmFailure.NotFound
        val check = checkDraft(draft)
        if (check != DraftCheck.Ok) {
            return when (check) {
                DraftCheck.Empty -> DmFailure.Invalid("Write something first.")
                DraftCheck.TooLong ->
                    DmFailure.Invalid("A message can be at most ${DmFormat.MaxLength} characters.")
                DraftCheck.Ok -> null
            }
        }
        sending = true
        sendFailure = null
        return try {
            val sent = api.send(conversationId, draft.trim())
            draft = ""
            if (sent != null) {
                val current = thread
                if (current is ThreadState.Ready) {
                    thread = current.copy(messages = current.messages + sent)
                }
            }
            null
        } catch (t: Throwable) {
            val failure = t.toDmFailure()
            if (failure is DmFailure.SubscriptionRequired) {
                expired = true
                val current = thread
                if (current is ThreadState.Ready) {
                    thread = current.copy(canReply = false)
                }
            }
            sendFailure = failure
            failure
        } finally {
            sending = false
        }
    }

    /** True once the server has told us the membership is gone. Cleared only by a reload. */
    var expired by mutableStateOf(false)
        private set

    suspend fun signIn(baseUrl: String, email: String, password: String): Boolean {
        signingIn = true
        signInError = null
        return try {
            session = api.signIn(baseUrl, email, password)
            expired = false
            refreshInbox()
            true
        } catch (t: Throwable) {
            signInError = t.toDmFailure().explain()
            false
        } finally {
            signingIn = false
        }
    }

    /** Re-reads the shared session after the global auth gate signs in or out. */
    fun syncSession() {
        val next = sessions.current()
        if (next.accessToken == session.accessToken && next.baseUrl == session.baseUrl) return
        session = next
        openConversationId = null
        openConversation = null
        thread = ThreadState.Loading
        inbox = if (next.signedIn) InboxState.Loading else InboxState.SignedOut
    }

    fun signOut() {
        sessions.signOut()
        session = sessions.current()
        inbox = InboxState.SignedOut
        thread = ThreadState.Loading
        openConversationId = null
        draft = ""
        expired = false
        sendFailure = null
    }

    fun clearSendFailure() {
        sendFailure = null
    }

    /** Starts a fresh tip attempt: one idempotency key per sheet, reused across retries. */
    fun startTip() {
        tipKey = UUID.randomUUID().toString()
        tipState = TipState.Idle
    }

    fun clearTip() {
        tipState = TipState.Idle
    }

    suspend fun sendTip(amountMinor: Long, note: String) {
        val conversation = openConversation
        if (conversation == null) {
            tipState = TipState.Failed("Open a conversation with a creator first.")
            return
        }
        when (checkTip(amountMinor)) {
            TipCheck.BelowMinimum -> {
                tipState = TipState.Failed("The smallest tip is ${money(MinTipMinor)}.")
                return
            }
            TipCheck.AboveMaximum -> {
                tipState = TipState.Failed("The largest tip is ${money(MaxTipMinor)}.")
                return
            }
            TipCheck.Ok -> Unit
        }
        if (tipKey.isBlank()) tipKey = UUID.randomUUID().toString()
        tipState = TipState.Sending
        tipState = try {
            val payment = api.tip(conversation.creatorId, amountMinor, note, tipKey)
            if (payment == null) {
                TipState.Failed("The server did not return a receipt. Nothing was charged.")
            } else {
                tipKey = ""
                TipState.Done(payment)
            }
        } catch (t: Throwable) {
            TipState.Failed(t.toDmFailure().explain())
        }
    }

    private fun peerFor(conversationId: String): Peer {
        val state = inbox
        if (state is InboxState.Ready) {
            state.items.firstOrNull { it.id == conversationId }?.let {
                return Peer(it.peerName, it.canReply)
            }
        }
        return Peer("Conversation", true)
    }

    private data class Peer(val name: String, val canReply: Boolean)
}
