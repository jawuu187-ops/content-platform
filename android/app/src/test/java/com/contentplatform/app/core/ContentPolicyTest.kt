package com.contentplatform.app.core

import com.contentplatform.app.core.ContentPolicy.Classification
import com.contentplatform.app.core.ContentPolicy.Surface
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ContentPolicyTest {

    @Test
    fun `public surface shows only safe content`() {
        assertTrue(ContentPolicy.isVisible(Surface.PUBLIC, Classification.SAFE, ageVerified = false, entitled = false))
        assertFalse(ContentPolicy.isVisible(Surface.PUBLIC, Classification.SUGGESTIVE, false, true))
        assertFalse(ContentPolicy.isVisible(Surface.PUBLIC, Classification.NUDITY, true, true))
    }

    @Test
    fun `paid surface requires both age verification and entitlement`() {
        assertFalse(ContentPolicy.isVisible(Surface.PAID, Classification.NUDITY, ageVerified = true, entitled = false))
        assertFalse(ContentPolicy.isVisible(Surface.PAID, Classification.NUDITY, ageVerified = false, entitled = true))
        assertTrue(ContentPolicy.isVisible(Surface.PAID, Classification.NUDITY, ageVerified = true, entitled = true))
        assertTrue(ContentPolicy.isVisible(Surface.PAID, Classification.SUGGESTIVE, true, true))
    }

    @Test
    fun `explicit and prohibited content is never visible`() {
        for (surface in Surface.entries) {
            for (verified in listOf(true, false)) {
                for (entitled in listOf(true, false)) {
                    assertFalse(ContentPolicy.isVisible(surface, Classification.EXPLICIT, verified, entitled))
                    assertFalse(ContentPolicy.isVisible(surface, Classification.PROHIBITED, verified, entitled))
                }
            }
        }
    }
}
