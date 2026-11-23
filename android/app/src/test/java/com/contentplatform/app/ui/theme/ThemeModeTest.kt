package com.contentplatform.app.ui.theme

import org.junit.Assert.assertEquals
import org.junit.Test

class ThemeModeTest {

    @Test
    fun `system is the default when nothing is saved`() {
        assertEquals(ThemeMode.System, ThemeMode.fromKey(null))
    }

    @Test
    fun `each mode survives a save and load round trip`() {
        ThemeMode.entries.forEach { mode ->
            assertEquals(mode, ThemeMode.fromKey(mode.storageKey))
        }
    }

    @Test
    fun `an unknown or corrupt value falls back to system`() {
        assertEquals(ThemeMode.System, ThemeMode.fromKey(""))
        assertEquals(ThemeMode.System, ThemeMode.fromKey("sepia"))
    }
}
