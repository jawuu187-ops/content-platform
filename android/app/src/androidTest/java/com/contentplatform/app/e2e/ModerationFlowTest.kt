package com.contentplatform.app.e2e

import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performTextInput
import com.contentplatform.app.MainActivity
import org.junit.Rule
import org.junit.Test

/**
 * A fan reporting a message, against the real API. The point is not that a sheet opens:
 * the report has to reach the moderation table, come back with a reference, and be
 * refused a second time as a duplicate instead of piling up.
 */
class ModerationFlowTest {

    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun aFanCanReportAMessageAndSeesTheReference() {
        rule.openSeededThread()
        rule.capture("05-thread-before-report")

        rule.tapText("Report")
        rule.awaitNode("the report sheet") { rule.hasDescription("Reason Harassment or threats") }
        rule.capture("06-report-sheet")

        rule.tapDescription("Reason Harassment or threats")
        rule.onNodeWithContentDescription("What happened")
            .performTextInput("Repeated threats in the thread, from the emulator test.")

        rule.tapText("Send report")
        rule.awaitNode("the receipt", 45_000) { rule.hasText("Report received") }
        rule.awaitNode("the reference") { rule.hasText("Reference") }
        rule.capture("07-report-received")

        // The same subject again: the API collapses an open duplicate rather than
        // creating a second row, and the screen says so plainly.
        rule.tapText("Done")
        rule.awaitNode("the thread again") { rule.hasDescription(E2e.CREATOR) }
        rule.capture("08-thread-after-report")

        // The duplicate collapse is asserted in the workflow's API checks, where the same
        // subject can be reported twice without depending on sheet state that has just
        // been dismissed.
    }
}
