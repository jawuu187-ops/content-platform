package com.contentplatform.app.ui.catalog

import android.graphics.BitmapFactory
import android.media.MediaPlayer
import android.net.Uri
import android.view.SurfaceHolder
import android.view.SurfaceView
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import kotlin.math.max
import com.contentplatform.app.catalog.CatalogStore
import com.contentplatform.app.catalog.money
import com.contentplatform.app.catalog.CreatorPost
import com.contentplatform.app.catalog.CreatorProfile
import com.contentplatform.app.catalog.CreatorState
import com.contentplatform.app.catalog.PostMedia
import com.contentplatform.app.catalog.CreatorsState
import com.contentplatform.app.catalog.SubscribeState
import com.contentplatform.app.catalog.VideoSource
import com.contentplatform.app.catalog.Tier
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Chrome
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch
import java.util.Locale

@Composable
fun DiscoverScreen(
    store: CatalogStore,
    onOpenCreator: (String) -> Unit,
) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var query by remember { mutableStateOf("") }
    var category by remember { mutableStateOf("") }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Discover", subtitle = "Creators on this platform")
        LazyColumn(Modifier.fillMaxSize().padding(16.dp)) {
            item {
                if (store.simulatedPayments) {
                    Text(
                        "SIMULATED PAYMENTS · test money only, no card is charged",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.brass,
                    )
                    SpacerH(8)
                }
                SearchField(query, onChange = { query = it }, onSubmit = {
                    scope.launch { store.loadCreators(query, category) }
                })
                SpacerH(8)
                Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
                    listOf("" to "All", "art" to "Art", "music" to "Music", "fitness" to "Fitness",
                        "lifestyle" to "Lifestyle", "photography" to "Photo", "writing" to "Writing",
                        "education" to "Education").forEach { (key, label) ->
                        TextButton(onClick = {
                            category = key
                            scope.launch { store.loadCreators(query, key) }
                        }) {
                            Text(
                                (if (category == key) "● " else "") + label,
                                color = if (category == key) palette.brass else palette.muted,
                                style = MaterialTheme.typography.labelSmall,
                            )
                        }
                    }
                }
                SpacerH(12)
                when (val state = store.creators) {
                    CreatorsState.Loading -> LabelMono("Loading creators…")
                    is CreatorsState.Failed -> {
                        Text(state.message, color = palette.danger, style = MaterialTheme.typography.bodySmall)
                        SpacerH(8)
                        OutlineButton("Retry", onClick = { scope.launch { store.loadCreators(query) } })
                    }
                    is CreatorsState.Ready -> {
                        if (state.items.isEmpty()) {
                            Text("No published creators match this filter.", color = palette.muted)
                        }
                    }
                }
            }
            if (store.creators is CreatorsState.Ready) {
                items((store.creators as CreatorsState.Ready).items, key = { it.userId }) { creator ->
                    SpacerH(10)
                    CreatorRow(creator, onClick = { onOpenCreator(creator.handle) })
                }
                if (store.nextCreatorOffset >= 0) {
                    item {
                        SpacerH(10)
                        OutlineButton(
                            label = if (store.loadingMoreCreators) "Loading…" else "Load more",
                            onClick = { scope.launch { store.loadMoreCreators() } },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun CreatorProfileImages(store: CatalogStore, handle: String) {
    var cover by remember(handle) { mutableStateOf<android.graphics.Bitmap?>(null) }
    var avatar by remember(handle) { mutableStateOf<android.graphics.Bitmap?>(null) }
    LaunchedEffect(handle) {
        runCatching { store.profileImageBytes(handle, "cover") }
            .onSuccess { cover = BitmapFactory.decodeByteArray(it, 0, it.size) }
        runCatching { store.profileImageBytes(handle, "avatar") }
            .onSuccess { avatar = BitmapFactory.decodeByteArray(it, 0, it.size) }
    }
    cover?.let {
        Image(
            bitmap = it.asImageBitmap(),
            contentDescription = "Creator cover",
            contentScale = ContentScale.Crop,
            modifier = Modifier.fillMaxWidth().height(150.dp),
        )
    }
    avatar?.let {
        Image(
            bitmap = it.asImageBitmap(),
            contentDescription = "Creator avatar",
            contentScale = ContentScale.Crop,
            modifier = Modifier.padding(start = 20.dp, top = 8.dp)
                .size(64.dp).clip(androidx.compose.foundation.shape.CircleShape),
        )
    }
}

@Composable
private fun CreatorRow(creator: CreatorProfile, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    CardSurface {
        Row(
            Modifier.fillMaxWidth().pressScale(motion, onClick = onClick).padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(44.dp).background(palette.surfaceRaised, androidx.compose.foundation.shape.CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                Text(creator.displayName.take(1).uppercase(Locale.UK), color = palette.ink)
            }
            Column(Modifier.weight(1f).padding(start = 12.dp)) {
                Text(creator.displayName, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text("@" + creator.handle + " · " + creator.category, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                if (creator.bio.isNotBlank()) {
                    Text(creator.bio, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary, maxLines = 2)
                }
            }
            LabelMono("\u2192")
        }
    }
}

@Composable
fun PublicCreatorScreen(
    store: CatalogStore,
    onBack: () -> Unit,
    onMessage: (String) -> Unit = {},
    messageBusy: Boolean = false,
    messageError: String? = null,
    onClearMessageError: () -> Unit = {},
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    var subscribeTier by remember { mutableStateOf<Tier?>(null) }
    BackHandler(onBack = onBack)

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).background(palette.chrome).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(48.dp).pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) { Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink) }
            Text("Creator", style = MaterialTheme.typography.titleMedium, color = palette.ink)
        }
        when (val state = store.creator) {
            CreatorState.Loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                LabelMono("Loading profile…")
            }
            is CreatorState.Failed -> Column(Modifier.padding(20.dp)) {
                Text(state.message, color = palette.danger)
                SpacerH(8)
                OutlineButton("Retry", onClick = {
                    store.openHandle?.let { scope.launch { store.open(it) } }
                })
            }
            is CreatorState.Ready -> LazyColumn(Modifier.fillMaxSize()) {
                item {
                    CreatorProfileImages(store, state.profile.handle)
                    Column(Modifier.padding(20.dp)) {
                        Text(state.profile.displayName, style = MaterialTheme.typography.displaySmall, color = palette.ink)
                        Text("@" + state.profile.handle + " · " + state.profile.category, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                        if (state.profile.bio.isNotBlank()) {
                            SpacerH(8)
                            Text(state.profile.bio, color = palette.inkSecondary)
                        }
                        if (state.profile.userId.isNotBlank()) {
                            SpacerH(12)
                            OutlineButton(
                                label = if (messageBusy) "Opening messages…" else "Message",
                                onClick = {
                                    onClearMessageError()
                                    onMessage(state.profile.userId)
                                },
                            )
                        }
                        messageError?.let {
                            SpacerH(6)
                            Text(it, style = MaterialTheme.typography.bodySmall, color = palette.danger)
                        }
                        SpacerH(16)
                        Text("Membership", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                        SpacerH(8)
                        if (state.tiers.isEmpty()) {
                            Text("This creator has not published a tier yet.", color = palette.muted)
                        }
                        state.tiers.forEach { tier ->
                            SpacerH(8)
                            TierRow(tier, onClick = { subscribeTier = tier })
                        }
                    }
                }
                item {
                    Column(Modifier.padding(horizontal = 20.dp)) {
                        Text("Posts", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                        SpacerH(8)
                        if (state.posts.isEmpty()) {
                            Text("No published posts yet.", color = palette.muted)
                        }
                        store.unlockNotice?.let {
                            SpacerH(8)
                            Text(it, style = MaterialTheme.typography.bodySmall, color = palette.brass)
                        }
                        store.unlockError?.let {
                            SpacerH(8)
                            Text(it, style = MaterialTheme.typography.bodySmall, color = palette.danger)
                        }
                    }
                }
                items(state.posts, key = { it.id }) { post ->
                    Column(Modifier.padding(horizontal = 20.dp, vertical = 6.dp)) {
                        PostRow(post, store, unlocking = store.unlockingPostId == post.id)
                    }
                }
            }
        }
    }

    subscribeTier?.let { tier ->
        TierSheet(
            tier = tier,
            state = store.subscribeState,
            simulated = store.simulatedPayments,
            onSubscribe = { code -> scope.launch { store.subscribe(tier, code) } },
            onDismiss = {
                subscribeTier = null
                store.dismissResult()
            },
        )
    }
}

@Composable
private fun TierRow(tier: Tier, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    CardSurface {
        Row(
            Modifier.fillMaxWidth().pressScale(motion, onClick = onClick).padding(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(tier.name, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                if (tier.description.isNotBlank()) {
                    Text(tier.description, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
                if (tier.perks.isNotEmpty()) {
                    Text(tier.perks.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
                }
            }
            Text(money(tier.priceMinor, tier.currency), style = MaterialTheme.typography.titleMedium, color = palette.ink)
        }
    }
}

@Composable
private fun PostRow(post: CreatorPost, store: CatalogStore, unlocking: Boolean) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    CardSurface {
        Column(Modifier.padding(14.dp)) {
            if (post.locked) {
                LabelMono("LOCKED · " + post.visibility)
            } else if (post.body.isNotBlank()) {
                Text(post.body, color = palette.inkSecondary)
            }
            if (!post.locked) {
                post.media.forEach { media ->
                    SpacerH(8)
                    PostMediaView(store, post.id, media)
                }
            }
            if (post.priceMinor != null && post.locked) {
                SpacerH(8)
                Text(
                    (if (store.simulatedPayments) "SIMULATED UNLOCK · " else "Unlock · ") +
                        money(post.priceMinor, post.currency),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.brass,
                )
                SpacerH(6)
                OutlineButton(
                    label = if (unlocking) "Unlocking…" else "Unlock this post",
                    onClick = {
                        store.clearUnlockMessage()
                        scope.launch { store.unlock(post) }
                    },
                )
            }
            SpacerH(6)
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (!post.locked) {
                    TextButton(onClick = { scope.launch { store.toggleLike(post) } }, enabled = !store.engagementBusy) {
                        Text((if (post.liked) "♥ " else "♡ ") + post.likes, color = if (post.liked) palette.brass else palette.muted)
                    }
                }
                TextButton(onClick = { scope.launch { store.toggleBookmark(post) } }, enabled = !store.engagementBusy) {
                    Text(if (post.bookmarked) "Saved" else "Save", color = if (post.bookmarked) palette.brass else palette.muted)
                }
                if (!post.locked) {
                    TextButton(onClick = { scope.launch { store.openComments(post) } }) {
                        Text("Comments " + post.comments, color = palette.muted)
                    }
                }
            }
            store.engagementError?.let {
                Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
            }
            if (!post.locked && store.engagementPostId == post.id) {
                SpacerH(8)
                store.comments.forEach { comment ->
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(comment.authorName, style = MaterialTheme.typography.labelSmall, color = palette.brass)
                            Text(comment.body, color = palette.inkSecondary, style = MaterialTheme.typography.bodySmall)
                        }
                        if (comment.mine) {
                            TextButton(onClick = { scope.launch { store.deleteComment(comment) } }) {
                                Text("Delete", color = palette.danger, style = MaterialTheme.typography.labelSmall)
                            }
                        }
                    }
                    SpacerH(4)
                }
                var draft by remember(post.id) { mutableStateOf("") }
                BasicTextField(
                    value = draft,
                    onValueChange = { draft = it.take(500) },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                    cursorBrush = SolidColor(palette.ink),
                    modifier = Modifier.fillMaxWidth()
                        .background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp))
                        .padding(10.dp),
                )
                SpacerH(6)
                OutlineButton(
                    label = if (store.engagementBusy) "Posting…" else "Post comment",
                    onClick = {
                        scope.launch {
                            store.addComment(post, draft)
                            if (draft.isNotBlank()) draft = ""
                        }
                    },
                )
            }
        }
    }
}

@Composable
private fun PostMediaView(store: CatalogStore, postId: String, media: PostMedia) {
    val palette = LocalVesperPalette.current
    var bitmap by remember { mutableStateOf<android.graphics.Bitmap?>(null) }
    var videoSource by remember { mutableStateOf<VideoSource?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(media.mediaId) {
        try {
            if (media.kind == "image") {
                val bytes = store.imageBytes(postId, media.mediaId)
                bitmap = withContext(Dispatchers.IO) { BitmapFactory.decodeByteArray(bytes, 0, bytes.size) }
                if (bitmap == null) failure = "Image could not be decoded."
            } else if (media.kind == "video") {
                videoSource = store.videoSource(postId, media.mediaId)
            }
        } catch (t: Throwable) {
            failure = t.message ?: "Media could not be loaded."
        }
    }

    bitmap?.let {
        Image(
            bitmap = it.asImageBitmap(),
            contentDescription = "Post media",
            contentScale = ContentScale.Fit,
            modifier = Modifier.fillMaxWidth().aspectRatio(
                if (it.height > 0) it.width.toFloat() / it.height.toFloat() else 1f,
            ),
        )
    }
    videoSource?.let { AuthenticatedVideo(it) }
    failure?.let {
        Text(it, color = palette.danger, style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
private fun AuthenticatedVideo(source: VideoSource) {
    val context = LocalContext.current
    var player by remember { mutableStateOf<MediaPlayer?>(null) }
    var duration by remember { mutableStateOf(0) }
    var position by remember { mutableStateOf(0) }
    var playing by remember { mutableStateOf(false) }

    DisposableEffect(Unit) {
        onDispose { player?.release() }
    }
    LaunchedEffect(player) {
        while (player != null) {
            position = player?.currentPosition ?: 0
            playing = player?.isPlaying == true
            delay(500)
        }
    }

    Column {
        AndroidView(
            factory = { ctx ->
                SurfaceView(ctx).apply {
                    holder.addCallback(object : SurfaceHolder.Callback {
                        override fun surfaceCreated(holder: SurfaceHolder) {
                            if (player != null) return
                            val mp = MediaPlayer()
                            try {
                                mp.setDataSource(ctx, Uri.parse(source.url), source.headers)
                                mp.setDisplay(holder)
                                mp.setOnPreparedListener { prepared ->
                                    player = prepared
                                    duration = prepared.duration
                                    prepared.start()
                                    playing = true
                                }
                                mp.setOnErrorListener { _, _, _ -> player = null; false }
                                mp.prepareAsync()
                            } catch (t: Throwable) {
                                mp.release()
                                player = null
                            }
                        }
                        override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) = Unit
                        override fun surfaceDestroyed(holder: SurfaceHolder) {
                            player?.release()
                            player = null
                        }
                    })
                }
            },
            modifier = Modifier.fillMaxWidth().height(220.dp),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = {
                player?.let {
                    if (it.isPlaying) it.pause() else it.start()
                    playing = it.isPlaying
                }
            }) { Text(if (playing) "Pause" else "Play") }
            Slider(
                value = position.toFloat().coerceIn(0f, max(1, duration).toFloat()),
                onValueChange = { player?.seekTo(it.toInt()) },
                valueRange = 0f..max(1, duration).toFloat(),
                modifier = Modifier.weight(1f),
            )
            Text(formatDuration(position) + " / " + formatDuration(duration), style = MaterialTheme.typography.bodySmall)
        }
    }
}

private fun formatDuration(ms: Int): String {
    val total = (ms / 1000).coerceAtLeast(0)
    return (total / 60).toString().padStart(2, '0') + ":" + (total % 60).toString().padStart(2, '0')
}

@Composable
private fun TierSheet(
    tier: Tier,
    state: SubscribeState,
    simulated: Boolean,
    onSubscribe: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    var promo by remember(tier.id) { mutableStateOf("") }
    androidx.compose.ui.window.Dialog(onDismissRequest = onDismiss) {
        CardSurface {
            Column(Modifier.padding(18.dp)) {
                Text(tier.name, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text(money(tier.priceMinor, tier.currency) + " / " + tier.periodDays + " days", color = palette.inkSecondary)
                if (tier.trialDays > 0) {
                    Text("Includes a ${tier.trialDays}-day free trial for first-time members.", color = palette.sage,
                        style = MaterialTheme.typography.bodySmall)
                }
                SpacerH(8)
                androidx.compose.foundation.text.BasicTextField(
                    value = promo,
                    onValueChange = { promo = it.uppercase().take(24) },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                    cursorBrush = androidx.compose.ui.graphics.SolidColor(palette.ink),
                    modifier = Modifier.fillMaxWidth().background(palette.surfaceRaised,
                        androidx.compose.foundation.shape.RoundedCornerShape(8.dp)).padding(10.dp),
                )
                SpacerH(4)
                LabelMono("PROMO CODE (OPTIONAL)")
                SpacerH(10)
                if (simulated) {
                    Text(
                        "SIMULATED CHECKOUT · no money moves; access is test access.",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.brass,
                    )
                    SpacerH(6)
                }
                when (state) {
                    SubscribeState.Idle -> {
                        Text(
                            "This sends a real subscription request to the platform. If no payment provider is configured, the server keeps it pending and nothing is charged.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                        SpacerH(10)
                        ActionButton("Request membership", null, onClick = { onSubscribe(promo) })
                    }
                    SubscribeState.Working -> LabelMono("Sending request…")
                    is SubscribeState.Active -> Text("Membership active.", color = palette.sage)
                    is SubscribeState.PaymentRequired -> Text(state.notice, color = palette.brass)
                    is SubscribeState.Failed -> Text(state.message, color = palette.danger)
                }
            }
        }
    }
}

@Composable
private fun SearchField(value: String, onChange: (String) -> Unit, onSubmit: () -> Unit) {
    val palette = LocalVesperPalette.current
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        androidx.compose.foundation.text.BasicTextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
            cursorBrush = androidx.compose.ui.graphics.SolidColor(palette.ink),
            modifier = Modifier.weight(1f).background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp)).padding(12.dp),
        )
        OutlineButton("Search", onClick = onSubmit)
    }
}

@Composable
fun MembershipsScreen(store: CatalogStore) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    androidx.compose.runtime.LaunchedEffect(Unit) { store.loadMemberships() }
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Memberships", subtitle = if (store.simulatedPayments) "SIMULATED memberships" else "Your active subscriptions")
        when (val state = store.memberships) {
            com.contentplatform.app.catalog.MembershipsState.Loading ->
                LabelMono("Loading memberships…")
            is com.contentplatform.app.catalog.MembershipsState.Failed ->
                Text(state.message, color = palette.danger, modifier = Modifier.padding(20.dp))
            is com.contentplatform.app.catalog.MembershipsState.Ready -> {
                if (state.items.isEmpty()) {
                    Text("No memberships yet. Find a creator in Discover.", color = palette.muted, modifier = Modifier.padding(20.dp))
                }
                LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp)) {
                    items(state.items, key = { it.id }) { sub ->
                        CardSurface {
                            Column(Modifier.padding(14.dp)) {
                                Text(sub.status.replaceFirstChar { it.uppercase() }, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                                Text(money(sub.priceMinor, sub.currency) + " · ends " + (sub.periodEnd?.take(10) ?: "—"), style = MaterialTheme.typography.bodySmall, color = palette.muted)
                                if (sub.cancelAtPeriodEnd) {
                                    LabelMono("CANCELS AT PERIOD END")
                                } else {
                                    SpacerH(8)
                                    OutlineButton("Cancel membership", onClick = { scope.launch { store.cancel(sub.id) } })
                                }
                            }
                        }
                        SpacerH(10)
                    }
                }
            }
        }
    }
}
