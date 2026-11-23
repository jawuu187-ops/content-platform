package com.contentplatform.app.ui.theme

import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Contrast is a design constraint, so it is a test, not a review note.
 *
 * Thresholds follow WCAG 2.1: 7:1 for body text, 4.5:1 for secondary text and accent type,
 * 3:1 for non-text accents. Both themes are held to the same numbers — dark is not allowed
 * to be the sloppier one.
 */
class VesperPaletteTest {

    private val themes = listOf("light" to VesperLight, "dark" to VesperDark)

    @Test
    fun `body text meets AAA on every surface`() {
        themes.forEach { (name, p) ->
            assertTrue("$name ink on ground", contrastRatio(p.ink, p.ground) >= 7.0)
            assertTrue("$name ink on surface", contrastRatio(p.ink, p.surface) >= 7.0)
            assertTrue("$name ink on chrome", contrastRatio(p.ink, p.chrome) >= 7.0)
        }
    }

    @Test
    fun `secondary text meets AA on every surface`() {
        themes.forEach { (name, p) ->
            assertTrue("$name inkSecondary on ground", contrastRatio(p.inkSecondary, p.ground) >= 7.0)
            assertTrue("$name muted on ground", contrastRatio(p.muted, p.ground) >= 4.5)
            assertTrue("$name muted on surface", contrastRatio(p.muted, p.surface) >= 4.5)
            assertTrue("$name muted on chrome", contrastRatio(p.muted, p.chrome) >= 4.5)
        }
    }

    @Test
    fun `action and status colours stay legible`() {
        themes.forEach { (name, p) ->
            assertTrue("$name onAction on action", contrastRatio(p.onAction, p.action) >= 4.5)
            assertTrue("$name actionText on ground", contrastRatio(p.actionText, p.ground) >= 4.5)
            assertTrue("$name sage on ground", contrastRatio(p.sage, p.ground) >= 3.0)
            assertTrue("$name brass on ground", contrastRatio(p.brass, p.ground) >= 3.0)
            assertTrue("$name danger on ground", contrastRatio(p.danger, p.ground) >= 4.5)
        }
    }

    @Test
    fun `oxblood stays oxblood in both themes`() {
        listOf("light action" to VesperLight.action, "dark action" to VesperDark.action,
            "dark actionText" to VesperDark.actionText).forEach { (name, c) ->
            assertTrue("$name keeps a red-dominant hue", c.red > c.green && c.green > c.blue)
            assertTrue("$name is not a neon pink", c.red < 0.95f)
        }
    }

    @Test
    fun `dark is designed, not an inversion`() {
        // A warm ground with a brown undertone, not pure black.
        assertTrue("dark ground is warm", VesperDark.ground.red >= VesperDark.ground.blue)
        assertTrue("dark ground is not black", relativeLuminance(VesperDark.ground) > 0.002)
        // Ink is bone-tinted rather than pure white.
        assertTrue("dark ink is tinted", VesperDark.ink.red > VesperDark.ink.blue)
        assertTrue("dark ink is not white", VesperDark.ink.red < 1.0f)
        // The three surfaces are distinguishable from one another.
        assertTrue(contrastRatio(VesperDark.surface, VesperDark.ground) > 1.1)
        assertTrue(contrastRatio(VesperDark.chrome, VesperDark.ground) > 1.03)
        // Light keeps its paper ground and does not drift towards grey.
        assertTrue("light ground is warm", VesperLight.ground.red > VesperLight.ground.blue)
    }

    @Test
    fun `the two themes are not the same palette`() {
        assertTrue(VesperLight.isDark != VesperDark.isDark)
        assertTrue(contrastRatio(VesperLight.ground, VesperDark.ground) > 10.0)
    }
}
