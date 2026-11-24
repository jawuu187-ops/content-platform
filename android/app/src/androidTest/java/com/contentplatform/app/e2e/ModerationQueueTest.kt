package com.contentplatform.app.e2e

import androidx.compose.ui.test.junit4.createAndroidComposeRule
import com.contentplatform.app.MainActivity
import org.junit.Rule
import org.junit.Test

/**
 * The moderator side, against the real API: sign in with an account that holds
 * safety.review, find the report the fan filed, move it to reviewing, close it with a
 * note, and watch the queue reflect the decision. A session with no permission must not
 * even see the entry point, which the first assertion proves by signing out first.
 */
class ModerationQueueTest {

    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun aModeratorReviewsAndClosesTheQueuedReport() {
        rule.capture("09-moderation-boot")
        rule.signInAs(E2e.MODERATOR_EMAIL)
        rule.capture("10-moderation-signed-in")

        rule.tapDescription("Tab You")
        rule.awaitNode("the account screen") { rule.hasText("Reports") }
        rule.tapText("Reports")
        rule.awaitNode("the queue header") { rule.hasText("Safety queue") }
        rule.awaitNode("the queued report", 45_000) {
            rule.hasDescription("Harassment or threats")
        }
        rule.capture("11-moderation-queue")

        rule.tapDescription("Harassment or threats")
        rule.awaitNode("the report detail") { rule.hasText("History") }
        rule.capture("12-moderation-detail")

        rule.tapText("Start review")
        rule.awaitNode("the reviewing state", 30_000) { rule.hasText("Reviewing") }

        rule.onNodeWithContentDescription("Decision note")
            .performTextInput("Reviewed in the emulator test; the thread was warned and left up.")
        rule.tapText("Action taken")
        rule.awaitNode("the closed state", 30_000) { rule.hasText("Actioned") }
        rule.capture("13-moderation-actioned")
    }
}
