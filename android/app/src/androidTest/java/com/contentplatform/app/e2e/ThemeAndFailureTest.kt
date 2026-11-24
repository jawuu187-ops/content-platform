package com.contentplatform.app.e2e

import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.contentplatform.app.MainActivity
import org.junit.Rule
import org.junit.Test

/**
 * Theme and failure states, checked on the real app.
 *
 * The theme assertion reads the rendered pixels rather than trusting a setting: the light
 * ground is near-white and the dark ground is near-black, so a patch of the screen proves
 * which one is actually painted.
 */
class ThemeAndFailureTest {

    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun darkThemeIsPaintedAndStillSends() {
        rule.openSeededThread()
        val light = rule.captureBitmap()?.brightnessAt(4, 400) ?: -1
        rule.capture("09-thread-light")
        if (light in 0..120) {
            throw AssertionError("expected the light theme, ground brightness was $light")
        }

        // You -> Dark.
        rule.onNodeWithContentDescription("Back").performClick()
        rule.onNodeWithContentDescription("Back").performClick()
        rule.awaitNode("the account screen") { rule.hasText("Appearance") }
        rule.onNodeWithText("Dark").performClick()
        rule.capture("10-account-dark")

        rule.openInbox()
        rule.awaitNode("the inbox in dark") { rule.hasDescription(E2e.CREATOR) }
        rule.onNodeWithContentDescription(E2e.CREATOR, substring = true).performClick()
        rule.awaitNode("the thread in dark") { rule.hasDescription(E2e.SEED_MESSAGE) }

        val dark = rule.captureBitmap()?.brightnessAt(4, 400) ?: -1
        rule.capture("11-thread-dark")
        if (dark !in 0..90) {
            throw AssertionError("expected the dark theme, ground brightness was $dark")
        }
        if (light - dark < 60) {
            throw AssertionError("light ($light) and dark ($dark) look too similar")
        }

        // The screen is not merely dark: it still works.
        val mine = "Dark theme message ${System.currentTimeMillis()}"
        rule.onNodeWithContentDescription("Write a message").performTextInput(mine)
        rule.onNodeWithText("Send").performClick()
        rule.awaitNode("the bubble in dark", 45_000) { rule.hasDescription("You: $mine") }

        // Leave the device in the light theme for whatever runs next.
        rule.onNodeWithContentDescription("Back").performClick()
        rule.onNodeWithContentDescription("Back").performClick()
        rule.awaitNode("the account screen again") { rule.hasText("Appearance") }
        rule.onNodeWithText("Light").performClick()
    }

    @Test
    fun offlineStateExplainsItselfAndRecovers() {
        rule.signInIfPrompted()
        try {
            shell("cmd connectivity airplane-mode enable")
            rule.activityRule.scenario.recreate()

            rule.openInbox()
            rule.awaitNode("the offline explanation", 60_000) { rule.hasText("You are offline") }
            rule.capture("12-offline")

            shell("cmd connectivity airplane-mode disable")
            rule.onNodeWithText("Try again").performClick()
            rule.awaitNode("the inbox once the network is back", 60_000) {
                rule.hasDescription(E2e.CREATOR) || rule.hasText("No conversations yet")
            }
            rule.capture("13-online-again")
        } finally {
            shell("cmd connectivity airplane-mode disable")
        }
    }
}
