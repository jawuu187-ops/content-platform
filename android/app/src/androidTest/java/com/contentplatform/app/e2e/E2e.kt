package com.contentplatform.app.e2e

import android.graphics.Bitmap
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.junit4.ComposeTestRule
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.printToLog
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.io.FileOutputStream

/**
 * Shared plumbing for the device tests.
 *
 * These tests drive the installed application against a real API that the workflow
 * starts on the runner's loopback interface; the emulator reaches it at 10.0.2.2. Nothing
 * here mocks a screen, a store or an HTTP call: if the app cannot log in, list a thread,
 * send, or read a receipt, the test fails.
 *
 * The fixture (one fan, one creator, one live membership, one message) is written by
 * backend/cmd/e2eseed, which refuses to run outside APP_ENV=demo with the fake provider.
 */
object E2e {
    /** The emulator's alias for the host loopback address. */
    const val BASE_URL = "http://10.0.2.2:8080"

    // Created by cmd/e2eseed. The account exists only in the disposable CI database, and
    // the password is supplied by the workflow as an instrumentation argument rather than
    // committed here.
    const val EMAIL = "fan@e2e.test"

    /** The fixture password passed as `-P...e2ePassword=...`; absent means misconfigured. */
    val password: String
        get() = InstrumentationRegistry.getArguments().getString("e2ePassword").orEmpty()
            .ifBlank { throw IllegalStateException("e2ePassword instrumentation argument is missing") }

    /** The moderator fixture, created and granted safety.review by cmd/e2eseed. */
    const val MODERATOR_EMAIL = "moderator@e2e.test"

    const val CREATOR = "Mira Vale"
    const val SEED_MESSAGE = "Welcome in. The new set is up"
    const val NO_E2EE_FRAGMENT = "does not use end-to-end encryption"
}

/** Waits until [condition] holds, failing with [what] after [timeoutMillis]. */
fun ComposeTestRule.awaitNode(what: String, timeoutMillis: Long = 30_000, condition: () -> Boolean) {
    val deadline = System.currentTimeMillis() + timeoutMillis
    while (System.currentTimeMillis() < deadline) {
        if (condition()) return
        Thread.sleep(200)
    }
    // The semantics tree is the difference between "the screen is wrong" and "the test
    // looked in the wrong place", so it goes to logcat next to the failure.
    try {
        onRoot().printToLog("E2E")
    } catch (t: Throwable) {
        println("semantics dump unavailable: ${t.message}")
    }
    throw AssertionError("timed out after ${timeoutMillis}ms waiting for $what")
}

fun ComposeTestRule.hasText(text: String): Boolean =
    onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty()

fun ComposeTestRule.hasDescription(text: String): Boolean =
    onAllNodesWithContentDescription(text, substring = true).fetchSemanticsNodes().isNotEmpty()

/**
 * Clicks the first node carrying [text]. Several surfaces legitimately repeat a word (a
 * tab and a heading, a label and a button), and demanding uniqueness would make these
 * tests brittle rather than strict: the assertions that matter are on what comes next.
 */
fun ComposeTestRule.tapText(text: String) {
    awaitNode("a control labelled $text") { hasText(text) }
    onAllNodesWithText(text, substring = false).onFirst().performClick()
}

fun ComposeTestRule.tapDescription(description: String) {
    awaitNode("a control described as $description") { hasDescription(description) }
    // Substring: a queue row describes itself as "reason, status, subject type".
    onAllNodesWithContentDescription(description, substring = true).onFirst().performClick()
}

/**
 * Signs in as [email] from the inbox, switching accounts when a session is already
 * present. The inbox header owns both "Sign out" and the sign-in panel, so this is the
 * only place that can change identity.
 */
fun ComposeTestRule.signInAs(email: String) {
    openInbox()
    awaitNode("the inbox header") {
        hasText("Sign out") || hasText("Connect an account") || hasDescription(E2e.CREATOR)
    }
    if (hasText("Sign out")) {
        tapText("Sign out")
        awaitNode("the sign-in panel") { hasText("Connect an account") }
    }
    if (hasText("Connect an account")) {
        onNodeWithContentDescription("Email").performTextInput(email)
        onNodeWithContentDescription("Password").performTextInput(E2e.password)
        tapText("Sign in")
    }
    awaitNode("a usable session", 45_000) {
        hasText("No conversations yet") || hasDescription(E2e.CREATOR) || hasText("You do not have")
    }
}

/** Opens You -> Inbox. Safe when the tab bar is already showing. */
fun ComposeTestRule.openInbox() {
    tapDescription("Tab You")
    awaitNode("the Inbox card") { hasText("Inbox") }
    tapText("Inbox")
}

/**
 * Signs in only when the panel is on screen. The session lives in the app's private
 * preferences, so later test classes reuse it instead of logging in again.
 */
fun ComposeTestRule.signInIfPrompted() {
    openInbox()
    awaitNode("either the sign-in panel or a loaded inbox") {
        hasText("Connect an account") || hasText("Sign out") || hasText("No conversations yet")
    }
    if (!hasText("Connect an account")) return

    onNodeWithContentDescription("Email").performTextInput(E2e.EMAIL)
    onNodeWithContentDescription("Password").performTextInput(E2e.password)
    tapText("Sign in")
    awaitNode("the signed-in inbox header") { hasText("Sign out") }
}

/** Opens the creator's conversation and waits until the seeded message is rendered. */
fun ComposeTestRule.openSeededThread() {
    signInIfPrompted()
    awaitNode("a conversation with ${E2e.CREATOR}") { hasDescription(E2e.CREATOR) }
    onNodeWithContentDescription(E2e.CREATOR, substring = true).performClick()
    awaitNode("the seeded message") { hasDescription(E2e.SEED_MESSAGE) }
}

/** The current screen as a bitmap, or null when capture is unavailable. */
fun ComposeTestRule.captureBitmap(): Bitmap? = try {
    onRoot().captureToImage().asAndroidBitmap()
} catch (t: Throwable) {
    println("screen capture skipped: ${t.message}")
    null
}

/** Average channel value of a patch: a blunt but reliable light/dark check. */
fun Bitmap.brightnessAt(x: Int, y: Int, size: Int = 12): Int {
    var total = 0L
    var samples = 0
    for (dx in 0 until size) {
        for (dy in 0 until size) {
            val px = getPixel((x + dx).coerceAtMost(width - 1), (y + dy).coerceAtMost(height - 1))
            total += (android.graphics.Color.red(px) + android.graphics.Color.green(px) +
                android.graphics.Color.blue(px)) / 3
            samples++
        }
    }
    return if (samples == 0) 0 else (total / samples).toInt()
}

/**
 * Closes the soft keyboard. The send and decision buttons sit under it on a 1080x2340
 * screen, and a tap that lands on the keyboard is silently swallowed: the form looked
 * filled in and nothing was sent.
 */
fun ComposeTestRule.hideKeyboard() {
    shell("input keyevent 4")
    Thread.sleep(400)
}

/** Runs a shell command on the device, used to take the emulator offline and back. */
fun shell(command: String) {
    val pfd = InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(command)
    android.os.ParcelFileDescriptor.AutoCloseInputStream(pfd).use { it.readBytes() }
    Thread.sleep(1_500)
}

/**
 * Writes a PNG of the current screen to the app's external files directory, which the
 * workflow pulls off the emulator. A screenshot problem never fails a test: the evidence is
 * useful, the assertion is the contract.
 */
fun ComposeTestRule.capture(name: String) {
    try {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        // Internal storage, not external: on a bare AVD getExternalFilesDir can be null,
        // and a screenshot that silently lands in "null/e2e" is worse than none. The
        // workflow pulls these with `run-as`, which works because the test APK is debug.
        val dir = File(context.filesDir, "e2e").apply { mkdirs() }
        val bitmap = onRoot().captureToImage().asAndroidBitmap()
        FileOutputStream(File(dir, "$name.png")).use { out ->
            bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
        bitmap.recycle()
    } catch (t: Throwable) {
        println("screenshot $name skipped: ${t.message}")
    }
}
