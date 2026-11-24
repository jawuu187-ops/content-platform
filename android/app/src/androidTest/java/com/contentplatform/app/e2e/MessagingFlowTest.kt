package com.contentplatform.app.e2e

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.contentplatform.app.MainActivity
import org.junit.Rule
import org.junit.Test

/**
 * The messaging workflow end to end: sign in, read the inbox, open the thread, send a
 * message, and see the same message come back from the server in the inbox preview.
 */
class MessagingFlowTest {

    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun signInInboxThreadAndSend() {
        rule.signInIfPrompted()
        rule.capture("01-inbox-signed-in")

        // Inbox: the creator, the seeded preview and its unread badge.
        rule.awaitNode("the creator row") { rule.hasDescription(E2e.CREATOR) }
        rule.onNodeWithContentDescription(E2e.CREATOR, substring = true).assertIsDisplayed()
        rule.awaitNode("the unread badge") { rule.hasText("new") }
        rule.awaitNode("the seeded preview") { rule.hasDescription(E2e.SEED_MESSAGE) }

        // Thread: the creator's message, the plain-language encryption note.
        rule.onNodeWithContentDescription(E2e.CREATOR, substring = true).performClick()
        rule.awaitNode("the thread") { rule.hasDescription("${E2e.CREATOR}: ${E2e.SEED_MESSAGE}") }
        rule.awaitNode("the no-encryption note") { rule.hasText(E2e.NO_E2EE_FRAGMENT) }
        rule.capture("02-thread")

        // Send, and wait for the bubble the server returned.
        val mine = "Sending this from the emulator test ${System.currentTimeMillis()}"
        rule.onNodeWithContentDescription("Write a message").performTextInput(mine)
        rule.tapText("Send")
        rule.awaitNode("the sent bubble", 45_000) { rule.hasDescription("You: $mine") }
        rule.capture("03-thread-sent")

        // Back to the inbox and refresh: the newest message is now ours.
        rule.onNodeWithContentDescription("Back").performClick()
        rule.awaitNode("the inbox after sending") { rule.hasText("Sign out") }
        rule.awaitNode("the inbox header") { rule.hasDescription(E2e.CREATOR) }

        // A fresh activity reads the inbox from the server, proving the preview is not
        // left over from local state.
        rule.activityRule.scenario.recreate()
        rule.openInbox()
        rule.awaitNode("the updated preview", 45_000) { rule.hasDescription(mine) }
        rule.capture("04-inbox-updated")
    }
}
