package com.contentplatform.app.e2e

import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import org.junit.Rule
import org.junit.Test

/**
 * Runs after the workflow ends the seeded membership. The requirement is not "the app
 * locks": history must stay readable, the reason must be on screen, and writing must stop.
 */
class ExpiredMembershipTest {

    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun readingSurvivesAndSendingIsPaused() {
        rule.signInIfPrompted()
        rule.awaitNode("the inbox") { rule.hasDescription(E2e.CREATOR) }
        // The row itself announces that the conversation is read only.
        rule.awaitNode("the read-only chip") { rule.hasText("Read only") }
        rule.onNodeWithContentDescription(E2e.CREATOR, substring = true).performClick()

        rule.awaitNode("the paused-membership notice") { rule.hasText("has ended") }
        rule.awaitNode("the earlier message") { rule.hasDescription(E2e.SEED_MESSAGE) }
        rule.awaitNode("the paused composer") { rule.hasDescription("Sending is paused") }
        rule.capture("14-expired-thread")

        // Pressing the send control cannot do anything: the composer is disabled and the
        // server refuses the write (proved by the workflow's API check). What matters here
        // is that the screen did not silently clear or lose the history.
        rule.onNodeWithContentDescription("Send").performClick()
        rule.awaitNode("the thread to stay put") { rule.hasDescription(E2e.SEED_MESSAGE) }
        if (!rule.hasText("has ended")) {
            throw AssertionError("the membership notice disappeared after a refused send")
        }
    }
}
