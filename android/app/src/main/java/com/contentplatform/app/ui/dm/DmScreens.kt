package com.contentplatform.app.ui.dm

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Image
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.contentplatform.app.dm.Conversation
import com.contentplatform.app.dm.DmFailure
import com.contentplatform.app.dm.DmFormat
import com.contentplatform.app.dm.DmStore
import com.contentplatform.app.dm.DraftCheck
import com.contentplatform.app.dm.InboxState
import com.contentplatform.app.dm.Message
import com.contentplatform.app.dm.ModerationStore
import com.contentplatform.app.dm.ThreadState
import com.contentplatform.app.dm.checkDraft
import com.contentplatform.app.dm.explain
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.Chip
import com.contentplatform.app.ui.components.ChipTone
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.Monogram
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.components.SpacerW
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * Direct messages: the inbox and one thread.
 *
 * Both surfaces are wired to the real endpoints through [DmStore]. Every state a request can
 * land in has its own presentation — loading, empty, offline, refused, expired membership — so
 * the screen never shows a blank list where the reason for the blankness matters.
 *
 * The copy never claims more privacy than the system has: the thread states plainly that
 * messages are stored server-side and are not end-to-end encrypted.
 */

// ---------------------------------------------------------------- inbox

@Composable
fun DmInboxScreen(
    store: DmStore,
    onBack: () -> Unit,
    onOpenThread: (String) -> Unit,
) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onBack)

    LaunchedEffect(store.signedIn) {
        if (store.signedIn) store.refreshInbox()
    }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        DmHeader(
            title = "Messages",
            subtitle = if (store.signedIn) "Direct messages" else "No account connected",
            onBack = onBack,
            trailing = if (store.signedIn) {
                { OutlineButton("Sign out", onClick = store::signOut) }
            } else {
                null
            },
        )

        when (val state = store.inbox) {
            InboxState.SignedOut -> SignInPanel(store = store, scope = scope)
            InboxState.Loading -> LoadingBlock("Loading conversations")
            InboxState.Empty -> EmptyBlock(
                title = "No conversations yet",
                body = "A conversation opens when you message a creator you are subscribed to, " +
                    "or when one messages you.",
                action = "Refresh",
                onAction = { scope.launch { store.refreshInbox() } },
            )
            is InboxState.Failed -> FailureBlock(
                failure = state.failure,
                onRetry = { scope.launch { store.refreshInbox() } },
            )
            is InboxState.Ready -> InboxList(
                items = state.items,
                onOpenThread = onOpenThread,
                onRefresh = { scope.launch { store.refreshInbox() } },
            )
        }
    }
}

@Composable
private fun InboxList(
    items: List<Conversation>,
    onOpenThread: (String) -> Unit,
    onRefresh: () -> Unit,
) {
    val now = remember { System.currentTimeMillis() }
    Column(Modifier.fillMaxSize()) {
        if (items.isEmpty()) {
            EmptyBlock("No conversations yet", "Nothing has been said yet.", "Refresh", onRefresh)
            return@Column
        }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 8.dp)) {
            items(items, key = { it.id }) { conversation ->
                InboxRow(conversation = conversation, now = now, onClick = { onOpenThread(conversation.id) })
                Divider()
            }
        }
    }
}

@Composable
private fun InboxRow(conversation: Conversation, now: Long, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val unread = conversation.unreadCount > 0
    val preview = when {
        conversation.lastMessageBody.isNotBlank() -> conversation.lastMessageBody
        else -> "No messages yet"
    }
    val stamp = conversation.lastMessageAtMillis?.let { DmFormat.relativeTime(now, it) } ?: ""
    val spoken = buildString {
        append(conversation.peerName)
        if (unread) append(", ${conversation.unreadCount} unread")
        append(", ")
        append(preview)
        if (stamp.isNotEmpty()) {
            append(", ")
            append(stamp)
        }
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .then(
                if (unread) Modifier.background(palette.surface.copy(alpha = 0.55f)) else Modifier,
            )
            .pressScale(motion, onClick = onClick)
            .semantics(mergeDescendants = true) { contentDescription = spoken }
            .padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Monogram(DmFormat.initials(conversation.peerName), size = 44)
        SpacerW(12)
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    conversation.peerName,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = if (unread) FontWeight.SemiBold else FontWeight.Normal,
                    color = palette.ink,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false),
                )
                if (unread) {
                    SpacerW(8)
                    Chip(
                        text = if (conversation.unreadCount > 9) "9+" else "${conversation.unreadCount} new",
                        tone = ChipTone.Action,
                    )
                }
            }
            SpacerH(3)
            Text(
                preview,
                style = MaterialTheme.typography.bodySmall,
                color = if (unread) palette.inkSecondary else palette.muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        SpacerW(12)
        Column(horizontalAlignment = Alignment.End) {
            if (stamp.isNotEmpty()) LabelMono(stamp)
            if (!conversation.canReply) {
                SpacerH(4)
                Chip("Read only", ChipTone.Pending)
            }
        }
    }
}

// ---------------------------------------------------------------- thread

@Composable
fun DmThreadScreen(
    store: DmStore,
    moderation: ModerationStore,
    onBack: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onBack)

    val state = store.thread
    var tipOpen by rememberSaveable { mutableStateOf(false) }
    var reportOpen by rememberSaveable { mutableStateOf(false) }
    // A report names the newest thing the other person said. When the thread is only ours,
    // the account itself is the subject, which is still a real, server-checked target.
    val peerMessage = (state as? ThreadState.Ready)?.messages?.lastOrNull { !it.mine }
    val reportSubjectType = if (peerMessage != null) "message" else "user"
    val reportSubjectId = peerMessage?.id ?: store.openConversation?.creatorId.orEmpty()
    val reportLabel = if (peerMessage != null) "this message" else "this account"
    LaunchedEffect(tipOpen) { if (tipOpen) store.startTip() }
    val listState = rememberLazyListState()
    LaunchedEffect(state) {
        if (state is ThreadState.Ready && state.messages.isNotEmpty()) {
            listState.animateScrollToItem(state.messages.lastIndex)
        }
    }

    Box(Modifier.fillMaxSize().background(palette.ground)) {
        Column(Modifier.fillMaxSize()) {
        DmHeader(
            title = (state as? ThreadState.Ready)?.peerName ?: "Conversation",
            subtitle = "Direct messages",
            onBack = onBack,
            trailing = {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    OutlineButton("Report", onClick = { reportOpen = true })
                    if (store.openConversation != null) {
                        SpacerW(6)
                        OutlineButton("Tip", onClick = { tipOpen = true })
                    }
                }
            },
        )

        when (state) {
            ThreadState.Loading -> LoadingBlock("Loading messages")
            is ThreadState.Failed -> FailureBlock(
                failure = state.failure,
                onRetry = { scope.launch { store.reloadThread() } },
            )
            is ThreadState.Ready -> {
                if (!state.canReply) {
                    Notice(
                        text = "Your membership to this creator has ended. You can still read " +
                            "everything below, but sending is paused until you resubscribe.",
                        tone = ChipTone.Pending,
                        modifier = Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
                    )
                }
                if (state.messages.isEmpty()) {
                    Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) {
                        EmptyBlock(
                            title = "Nothing here yet",
                            body = "Say hello. Your first message starts the thread.",
                            action = null,
                            onAction = {},
                        )
                    }
                } else {
                    LazyColumn(
                        modifier = Modifier.weight(1f).fillMaxWidth(),
                        state = listState,
                        contentPadding = PaddingValues(horizontal = 20.dp, vertical = 16.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        items(state.messages, key = { it.id }) { message ->
                            MessageBubble(
                                store = store,
                                message = message,
                                peerName = state.peerName,
                                now = System.currentTimeMillis(),
                            )
                        }
                        item {
                            SpacerH(4)
                            Text(
                                DmFormat.NO_E2EE_NOTE,
                                style = MaterialTheme.typography.bodySmall,
                                color = palette.muted,
                            )
                        }
                    }
                }
                Composer(store = store, canReply = state.canReply, scope = scope)
            }
        }
        }
        if (reportOpen) {
            ReportSheet(
                store = moderation,
                subjectType = reportSubjectType,
                subjectId = reportSubjectId,
                subjectLabel = reportLabel,
                scope = scope,
                onDismiss = {
                    reportOpen = false
                    moderation.clearReport()
                },
            )
        }
        if (tipOpen) {
            TipSheet(
                peerName = (state as? ThreadState.Ready)?.peerName ?: "this creator",
                store = store,
                scope = scope,
                onDismiss = {
                    tipOpen = false
                    store.clearTip()
                },
            )
        }
    }
}

@Composable
private fun MessageBubble(store: DmStore, message: Message, peerName: String, now: Long) {
    val palette = LocalVesperPalette.current
    var attachment by remember(message.mediaId) { mutableStateOf<android.graphics.Bitmap?>(null) }
    LaunchedEffect(message.mediaId) {
        if (message.mediaId.isNotBlank()) {
            runCatching { store.loadMedia(message.id) }.onSuccess {
                attachment = android.graphics.BitmapFactory.decodeByteArray(it, 0, it.size)
            }
        }
    }
    val shape = RoundedCornerShape(
        topStart = 14.dp,
        topEnd = 14.dp,
        bottomStart = if (message.mine) 14.dp else 4.dp,
        bottomEnd = if (message.mine) 4.dp else 14.dp,
    )
    val container = if (message.mine) palette.action else palette.surface
    val textColor = if (message.mine) palette.onAction else palette.ink
    val metaColor = if (message.mine) palette.onAction.copy(alpha = 0.75f) else palette.muted
    val spoken = buildString {
        append(if (message.mine) "You" else peerName)
        append(": ")
        append(message.body)
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = if (message.mine) Arrangement.End else Arrangement.Start,
    ) {
        Column(
            modifier = Modifier
                .widthIn(max = 300.dp)
                .clip(shape)
                .background(container)
                .border(1.dp, palette.rule, shape)
                .semantics(mergeDescendants = true) { contentDescription = spoken }
                .padding(horizontal = 12.dp, vertical = 9.dp),
        ) {
            attachment?.let {
                androidx.compose.foundation.Image(
                    bitmap = it.asImageBitmap(),
                    contentDescription = "Message attachment",
                    contentScale = androidx.compose.ui.layout.ContentScale.Fit,
                    modifier = Modifier.fillMaxWidth(),
                )
                SpacerH(4)
            }
            if (message.body.isNotBlank()) {
                Text(message.body, style = MaterialTheme.typography.bodyMedium, color = textColor)
            }
            SpacerH(4)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LabelMono(DmFormat.threadTime(now, message.sentAtMillis), color = metaColor)
                if (message.mine) {
                    LabelMono(if (message.read) "Read" else "Sent", color = metaColor)
                }
            }
        }
    }
}

@Composable
private fun Composer(store: DmStore, canReply: Boolean, scope: CoroutineScope) {
    val palette = LocalVesperPalette.current
    val context = androidx.compose.ui.platform.LocalContext.current
    val picker = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.activity.result.contract.ActivityResultContracts.GetContent(),
    ) { uri ->
        if (uri != null) {
            scope.launch {
                val resolver = context.contentResolver
                val mime = resolver.getType(uri) ?: "image/jpeg"
                resolver.openInputStream(uri)?.use { input ->
                    store.sendImage(input.readBytes(), mime)
                }
            }
        }
    }
    val failure = store.sendFailure
    val check = checkDraft(store.draft)
    val canSubmit = canReply && !store.sending && check == DraftCheck.Ok

    Column(
        Modifier
            .fillMaxWidth()
            .background(palette.chrome)
            .imePadding(),
    ) {
        Divider()
        Column(Modifier.padding(horizontal = 20.dp, vertical = 12.dp)) {
            if (failure != null) {
                Notice(
                    text = failure.explain(),
                    tone = if (failure == DmFailure.Offline) ChipTone.Pending else ChipTone.Danger,
                )
                SpacerH(8)
            }
            Row(verticalAlignment = Alignment.Bottom) {
                TextButton(onClick = { picker.launch("image/*") }, enabled = canReply && !store.sending) {
                    Text("Attach", color = palette.actionText)
                }
                Box(Modifier.weight(1f)) {
                    DmField(
                        value = store.draft,
                        onValueChange = { store.draft = it.take(DmFormat.MaxLength + 200) },
                        placeholder = if (canReply) "Write a message" else "Sending is paused",
                        enabled = canReply && !store.sending,
                        singleLine = false,
                        imeAction = ImeAction.Default,
                    )
                }
                SpacerW(8)
                Box(Modifier.width(112.dp)) {
                    ActionButton(
                        label = when {
                            store.sending -> "Sending"
                            failure != null -> "Retry"
                            else -> "Send"
                        },
                        price = null,
                        enabled = canSubmit,
                        onClick = { scope.launch { store.send() } },
                    )
                }
            }
            SpacerH(6)
            Row(verticalAlignment = Alignment.CenterVertically) {
                LabelMono("${DmFormat.length(store.draft)} / ${DmFormat.MaxLength}")
                Spacer(Modifier.weight(1f))
                if (check == DraftCheck.TooLong) {
                    LabelMono("Too long", color = palette.danger)
                }
            }
        }
    }
}

// ---------------------------------------------------------------- sign in

@Composable
private fun SignInPanel(store: DmStore, scope: CoroutineScope) {
    val palette = LocalVesperPalette.current
    var baseUrl by rememberSaveable { mutableStateOf(store.session.baseUrl.ifBlank { "http://10.0.2.2:8080" }) }
    var email by rememberSaveable { mutableStateOf(store.session.email) }
    var password by rememberSaveable { mutableStateOf("") }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
    ) {
        Text(
            "Connect an account",
            style = MaterialTheme.typography.titleMedium,
            color = palette.ink,
        )
        SpacerH(6)
        Text(
            "Messages live on the server, so the app needs a session. Sign in with an " +
                "account on this platform. The server address is remembered on this device.",
            style = MaterialTheme.typography.bodySmall,
            color = palette.muted,
        )
        SpacerH(16)
        DmField(
            value = baseUrl,
            onValueChange = { baseUrl = it },
            placeholder = "Server address",
            label = "Server",
            enabled = !store.signingIn,
            singleLine = true,
        )
        SpacerH(10)
        DmField(
            value = email,
            onValueChange = { email = it },
            placeholder = "you@example.com",
            label = "Email",
            enabled = !store.signingIn,
            singleLine = true,
        )
        SpacerH(10)
        DmField(
            value = password,
            onValueChange = { password = it },
            placeholder = "Password",
            label = "Password",
            enabled = !store.signingIn,
            singleLine = true,
            password = true,
            imeAction = ImeAction.Done,
            onDone = {
                if (!store.signingIn) scope.launch { store.signIn(baseUrl, email, password) }
            },
        )
        SpacerH(8)
        LabelMono("Use http:// only for a local server. Deployed servers must be https.")
        val error = store.signInError
        if (error != null) {
            SpacerH(12)
            Notice(text = error, tone = ChipTone.Danger)
        }
        SpacerH(16)
        ActionButton(
            label = if (store.signingIn) "Signing in" else "Sign in",
            price = null,
            enabled = !store.signingIn && email.isNotBlank() && password.isNotEmpty(),
            onClick = { scope.launch { store.signIn(baseUrl, email, password) } },
        )
        SpacerH(12)
        Text(
            DmFormat.NO_E2EE_NOTE,
            style = MaterialTheme.typography.bodySmall,
            color = palette.muted,
        )
    }
}

// ---------------------------------------------------------------- shared pieces

@Composable
private fun DmHeader(
    title: String,
    subtitle: String,
    onBack: () -> Unit,
    trailing: (@Composable () -> Unit)? = null,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(Modifier.fillMaxWidth().background(palette.chrome)) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) {
                Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink)
            }
            Column(Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleMedium, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(subtitle, style = MaterialTheme.typography.bodySmall, color = palette.muted, maxLines = 1)
            }
            trailing?.invoke()
        }
        Divider()
    }
}

@Composable
private fun DmField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    label: String? = null,
    enabled: Boolean = true,
    singleLine: Boolean = true,
    password: Boolean = false,
    imeAction: ImeAction = ImeAction.Next,
    onDone: (() -> Unit)? = null,
) {
    val palette = LocalVesperPalette.current
    Column(Modifier.fillMaxWidth()) {
        if (label != null) {
            LabelMono(label)
            SpacerH(4)
        }
        Box(
            Modifier
                .fillMaxWidth()
                .heightIn(min = 48.dp)
                .clip(RoundedCornerShape(8.dp))
                .background(palette.surface)
                .border(1.dp, palette.ruleStrong, RoundedCornerShape(8.dp))
                .padding(horizontal = 12.dp, vertical = 12.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            BasicTextField(
                value = value,
                onValueChange = onValueChange,
                enabled = enabled,
                singleLine = singleLine,
                textStyle = MaterialTheme.typography.bodyMedium.copy(
                    color = if (enabled) palette.ink else palette.muted,
                ),
                cursorBrush = SolidColor(palette.action),
                visualTransformation = if (password) {
                    PasswordVisualTransformation()
                } else {
                    VisualTransformation.None
                },
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.Sentences,
                    imeAction = imeAction,
                ),
                keyboardActions = KeyboardActions(
                    onDone = { onDone?.invoke() },
                    onSend = { onDone?.invoke() },
                ),
                modifier = Modifier.fillMaxWidth().semantics {
                    contentDescription = label ?: placeholder
                },
                decorationBox = { innerTextField ->
                    Box(Modifier.fillMaxWidth()) {
                        if (value.isEmpty()) {
                            Text(
                                placeholder,
                                style = MaterialTheme.typography.bodyMedium,
                                color = palette.muted,
                            )
                        }
                        innerTextField()
                    }
                },
            )
        }
    }
}

@Composable
private fun LoadingBlock(label: String) {
    val palette = LocalVesperPalette.current
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(22.dp),
            color = palette.action,
            strokeWidth = 2.dp,
        )
        SpacerH(12)
        Text(label, style = MaterialTheme.typography.bodySmall, color = palette.muted)
    }
}

@Composable
private fun EmptyBlock(
    title: String,
    body: String,
    action: String?,
    onAction: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(title, style = MaterialTheme.typography.titleMedium, color = palette.ink)
        SpacerH(6)
        Text(body, style = MaterialTheme.typography.bodySmall, color = palette.muted)
        if (action != null) {
            SpacerH(16)
            OutlineButton(action, onClick = onAction)
        }
    }
}

@Composable
private fun FailureBlock(failure: DmFailure, onRetry: () -> Unit) {
    val palette = LocalVesperPalette.current
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            if (failure == DmFailure.Offline) "You are offline" else "That did not load",
            style = MaterialTheme.typography.titleMedium,
            color = palette.ink,
        )
        SpacerH(6)
        Text(failure.explain(), style = MaterialTheme.typography.bodySmall, color = palette.muted)
        SpacerH(16)
        OutlineButton("Try again", onClick = onRetry)
    }
}

@Composable
private fun Notice(text: String, tone: ChipTone, modifier: Modifier = Modifier) {
    val palette = LocalVesperPalette.current
    val accent = when (tone) {
        ChipTone.Danger -> palette.danger
        ChipTone.Pending -> palette.brass
        ChipTone.Paid -> palette.sage
        ChipTone.Action -> palette.action
        ChipTone.Neutral -> palette.muted
    }
    Row(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(accent.copy(alpha = 0.14f))
            .border(1.dp, accent.copy(alpha = 0.36f), RoundedCornerShape(8.dp))
            .padding(horizontal = 10.dp, vertical = 8.dp),
    ) {
        Text(text, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
    }
}
