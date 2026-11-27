package com.contentplatform.app.ui.catalog

import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.contentplatform.app.catalog.CreatorStudioStore
import com.contentplatform.app.catalog.CreatorToolsState
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

@Composable
fun CreatorToolsScreen(store: CreatorStudioStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { store.load() }

    Column(Modifier.fillMaxSize().background(palette.ground).imePadding()) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).background(palette.chrome).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(48.dp).pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) { Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink) }
            Column {
                Text("Creator studio", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text("Publish and manage your page", style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
        }
        when (val state = store.state) {
            CreatorToolsState.Loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                LabelMono("Loading…")
            }
            is CreatorToolsState.Failed -> Column(Modifier.padding(20.dp)) {
                Text(state.message, color = palette.danger)
                SpacerH(8)
                OutlineButton("Retry", onClick = { scope.launch { store.load() } })
            }
            CreatorToolsState.NoProfile -> Onboarding(store)
            is CreatorToolsState.Ready -> ReadyTools(store, state)
        }
    }
}

@Composable
private fun Onboarding(store: CreatorStudioStore) {
    var handle by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }
    var bio by remember { mutableStateOf("") }
    var category by remember { mutableStateOf("photography") }
    val scope = rememberCoroutineScope()
    val palette = LocalVesperPalette.current
    LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(20.dp)) {
        item {
            Text("Create your creator page", style = MaterialTheme.typography.headlineSmall, color = palette.ink)
            SpacerH(6)
            Text(
                "Your page stays a draft until you publish it. Publishing requires the creator role, which the server grants with the profile.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
            SpacerH(14)
            ToolsField("Handle", handle, { handle = it.lowercase().filter { c -> c.isLetterOrDigit() || c == '_' }.take(30) }, "your_handle")
            SpacerH(10)
            ToolsField("Display name", name, { name = it }, "Your name")
            SpacerH(10)
            ToolsField("Bio", bio, { bio = it }, "What you publish")
            SpacerH(10)
            ToolsField("Category", category, { category = it }, "photography")
            SpacerH(14)
            ActionButton("Create page", null, onClick = {
                scope.launch { store.createProfile(handle, name, bio, category, false) }
            }, enabled = !store.busy && handle.length >= 3 && name.isNotBlank())
            store.message?.let { SpacerH(10); Text(it, color = palette.danger) }
        }
    }
}

@Composable
private fun ReadyTools(store: CreatorStudioStore, state: CreatorToolsState.Ready) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var name by remember(state.profile.handle) { mutableStateOf(state.profile.displayName) }
    var bio by remember(state.profile.handle) { mutableStateOf(state.profile.bio) }
    var category by remember(state.profile.handle) { mutableStateOf(state.profile.category) }
    var published by remember(state.profile.handle) { mutableStateOf(state.profile.published) }

    var tierName by remember { mutableStateOf("") }
    var tierPrice by remember { mutableStateOf("") }
    var tierDays by remember { mutableStateOf("30") }

    var body by remember { mutableStateOf("") }
    var visibility by remember { mutableStateOf("public") }
    var price by remember { mutableStateOf("") }
    var picked by remember { mutableStateOf<Uri?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { picked = it }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(20.dp)) {
        item {
            Text("@" + state.profile.handle, style = MaterialTheme.typography.titleMedium, color = palette.ink)
            LabelMono(if (state.profile.published) "PUBLISHED" else "DRAFT")
            SpacerH(14)
            CardSurface {
                Column(Modifier.padding(14.dp)) {
                    ToolsField("Display name", name, { name = it }, "")
                    SpacerH(8)
                    ToolsField("Bio", bio, { bio = it }, "")
                    SpacerH(8)
                    ToolsField("Category", category, { category = it }, "")
                    SpacerH(10)
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        OutlineButton(if (published) "Unpublish" else "Publish", onClick = { published = !published })
                        SpacerH(8)
                        ActionButton("Save profile", null, onClick = {
                            scope.launch { store.saveProfile(name, bio, category, published) }
                        }, enabled = !store.busy)
                    }
                }
            }
            SpacerH(18)
            Text("Tiers", style = MaterialTheme.typography.headlineSmall, color = palette.ink)
            state.tiers.forEach { tier ->
                SpacerH(6)
                CardSurface {
                    Row(Modifier.fillMaxWidth().padding(12.dp)) {
                        Text(tier.name + " · " + money(tier.priceMinor, tier.currency), color = palette.ink, modifier = Modifier.weight(1f))
                        LabelMono(if (tier.active) "ACTIVE" else "OFF")
                    }
                }
            }
            SpacerH(10)
            CardSurface {
                Column(Modifier.padding(14.dp)) {
                    Text("Add a tier", color = palette.ink)
                    ToolsField("Name", tierName, { tierName = it }, "Studio")
                    SpacerH(6)
                    ToolsField("Price in cents", tierPrice, { tierPrice = it.filter(Char::isDigit).take(9) }, "900", KeyboardType.Number)
                    SpacerH(6)
                    ToolsField("Period days", tierDays, { tierDays = it.filter(Char::isDigit).take(3) }, "30", KeyboardType.Number)
                    SpacerH(8)
                    ActionButton("Create tier", null, onClick = {
                        scope.launch {
                            store.addTier(
                                tierName,
                                "",
                                tierPrice.toLongOrNull() ?: 0,
                                "USD",
                                tierDays.toIntOrNull() ?: 30,
                            )
                        }
                    }, enabled = !store.busy && tierName.isNotBlank() && (tierPrice.toLongOrNull() ?: 0) > 0)
                }
            }
            SpacerH(18)
            Text("Publish a post", style = MaterialTheme.typography.headlineSmall, color = palette.ink)
            SpacerH(8)
            CardSurface {
                Column(Modifier.padding(14.dp)) {
                    ToolsField("Caption", body, { body = it }, "What is this post?")
                    SpacerH(8)
                    Row {
                        listOf("public", "subscriber", "ppv").forEach { value ->
                            OutlineButton(
                                if (visibility == value) "• $value" else value,
                                onClick = { visibility = value },
                            )
                            SpacerH(6)
                        }
                    }
                    if (visibility == "ppv") {
                        SpacerH(6)
                        ToolsField("Unlock price in cents", price, { price = it.filter(Char::isDigit).take(9) }, "500", KeyboardType.Number)
                    }
                    SpacerH(8)
                    OutlineButton(if (picked == null) "Choose image or video" else "Media selected", onClick = {
                        picker.launch("*/*")
                    })
                    if (picked != null) {
                        SpacerH(4)
                        LabelMono("A file is attached; it uploads when you publish.")
                    }
                    SpacerH(8)
                    ActionButton("Publish", null, onClick = {
                        scope.launch {
                            store.publish(
                                picked,
                                body,
                                visibility,
                                if (visibility == "ppv") price.toLongOrNull() else null,
                                "USD",
                            )
                            if (store.message == "Post published.") {
                                picked = null
                                body = ""
                            }
                        }
                    }, enabled = !store.busy && (body.isNotBlank() || picked != null))
                }
            }
            store.message?.let {
                SpacerH(10)
                Text(it, color = if (it == "Post published.") palette.sage else palette.danger)
            }
        }
    }
}

@Composable
private fun ToolsField(
    label: String,
    value: String,
    onChange: (String) -> Unit,
    placeholder: String,
    keyboardType: KeyboardType = KeyboardType.Text,
) {
    val palette = LocalVesperPalette.current
    Column {
        LabelMono(label)
        SpacerH(4)
        Box(
            Modifier.fillMaxWidth().height(44.dp)
                .background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp))
                .padding(horizontal = 12.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (value.isBlank() && placeholder.isNotBlank()) {
                Text(placeholder, style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
            BasicTextField(
                value = value,
                onValueChange = onChange,
                singleLine = true,
                textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                cursorBrush = SolidColor(palette.ink),
                keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = ImeAction.Next),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
