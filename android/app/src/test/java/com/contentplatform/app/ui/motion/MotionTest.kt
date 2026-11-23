package com.contentplatform.app.ui.motion

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Reduced motion is a contract, not a preference: if either Android animation scale is
 * zero, every duration must resolve to zero so transitions complete instantly and no
 * content animates in.
 */
class MotionTest {

    @Test
    fun `either scale at zero disables motion`() {
        assertTrue(resolveReducedMotion(0f, 1f))
        assertTrue(resolveReducedMotion(1f, 0f))
        assertTrue(resolveReducedMotion(0f, 0f))
        assertFalse(resolveReducedMotion(1f, 1f))
        assertFalse(resolveReducedMotion(0.5f, 0.5f))
    }

    @Test
    fun `reduced motion collapses every duration and stagger`() {
        val reduced = VesperMotion(reduced = true)
        assertEquals(0, reduced.feedback)
        assertEquals(0, reduced.transition)
        assertEquals(0, reduced.reveal)
        assertEquals(0L, reduced.stagger(5))
    }

    @Test
    fun `motion vocabulary is three short durations`() {
        val motion = VesperMotion()
        assertEquals(140, motion.feedback)
        assertEquals(220, motion.transition)
        assertEquals(320, motion.reveal)
        // Nothing may delay an action for longer than a third of a second.
        assertTrue(motion.reveal <= 320)
    }

    @Test
    fun `stagger is capped so a long list never waits`() {
        val motion = VesperMotion()
        assertEquals(0L, motion.stagger(0))
        assertEquals(200L, motion.stagger(5))
        assertEquals(240L, motion.stagger(6))
        assertEquals(240L, motion.stagger(40))
    }
}
