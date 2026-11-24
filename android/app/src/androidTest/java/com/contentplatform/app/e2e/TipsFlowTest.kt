package com.contentplatform.app.e2e

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import com.contentplatform.app.MainActivity
import org.junit.Rule
import org.junit.Test

/**
 * The tipping workflow: presets, a custom amount, both bounds, a sent tip, and a receipt
 * that carries the server's own TEST status.
 *
 * The last assertion matters most: after the receipt is rendered the send control is gone,
 * so the same sheet cannot be submitted twice. The workflow's API check separately proves
 * that repeating one idempotency key returns the same intent instead of a second charge.
 */
class TipsFlowTest {

    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun presetsBoundsAndSimulatedReceipt() {
        rule.openSeededThread()

        rule.onNodeWithText("Tip").performClick()
        rule.awaitNode("the tip sheet") { rule.hasText("Send a tip") }
        rule.capture("05-tip-open")

        // A preset is selected by default and is immediately sendable.
        rule.onNodeWithContentDescription("Tip $5.00").assertIsDisplayed()
        rule.onNodeWithText("Send $5.00").assertIsDisplayed()

        // Choosing another preset reprices the action.
        rule.onNodeWithContentDescription("Tip $10.00").performClick()
        rule.awaitNode("the repriced action") { rule.hasText("Send $10.00") }

        // Below the minimum: explained before the request, and the action refuses.
        rule.onNodeWithContentDescription("Other amount, e.g. 15").performTextInput("0.5")
        rule.awaitNode("the lower bound message") { rule.hasText("The smallest tip is $1.00") }
        rule.onNodeWithText("Send $0.50").performClick()
        rule.awaitNode("the sheet to stay open") { rule.hasText("Send a tip") }

        // Above the maximum.
        rule.onNodeWithContentDescription("Other amount, e.g. 15").performTextClearance()
        rule.onNodeWithContentDescription("Other amount, e.g. 15").performTextInput("9999")
        rule.awaitNode("the upper bound message") { rule.hasText("The largest tip is $5000.00") }
        rule.capture("06-tip-too-large")

        // A valid custom amount, with a note, sent for real against the API.
        rule.onNodeWithContentDescription("Other amount, e.g. 15").performTextClearance()
        rule.onNodeWithContentDescription("Other amount, e.g. 15").performTextInput("12.34")
        rule.onNodeWithContentDescription("Say what it is for").performTextInput("emulator happy path")
        rule.awaitNode("a sendable custom amount") { rule.hasText("Send $12.34") }
        rule.onNodeWithText("Send $12.34").performClick()

        rule.awaitNode("the receipt", 45_000) { rule.hasText("Tip sent") }
        rule.awaitNode("the simulated TEST banner") { rule.hasText("Test payment") }
        rule.awaitNode("the reference") { rule.hasText("Reference") }
        rule.awaitNode("the receipt amount") { rule.hasText("$12.34") }
        rule.capture("07-tip-receipt")

        // The receipt replaces the form: there is no control left that could send again.
        if (rule.hasText("Send $12.34")) {
            throw AssertionError("the send control survived the receipt and could double-submit")
        }

        rule.onNodeWithText("Done").performClick()
        rule.awaitNode("the thread after the receipt") { rule.hasDescription(E2e.SEED_MESSAGE) }
        rule.capture("08-thread-after-tip")
    }
}
