package com.contentplatform.app.ui.theme

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * Theme preference: follow the system, or pin light or dark.
 *
 * The stored value is the preference, not the resolved theme, so "System" keeps following
 * the device when the user changes it at night without reopening the app.
 */
enum class ThemeMode(val storageKey: String) {
    System("system"),
    Light("light"),
    Dark("dark"),
    ;

    companion object {
        fun fromKey(key: String?): ThemeMode =
            entries.firstOrNull { it.storageKey == key } ?: System
    }
}

/**
 * Persists the preference and exposes it as observable Compose state.
 *
 * SharedPreferences rather than DataStore on purpose: one key, read once at startup, no
 * coroutine scope and no extra dependency. The theme must be correct on the first frame,
 * before any async work runs.
 */
class ThemeController(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    var mode: ThemeMode by mutableStateOf(ThemeMode.fromKey(prefs.getString(KEY_MODE, null)))
        private set

    fun choose(newMode: ThemeMode) {
        if (newMode == mode) return
        mode = newMode
        prefs.edit().putString(KEY_MODE, newMode.storageKey).apply()
    }

    private companion object {
        const val PREFS = "vesper.ui"
        const val KEY_MODE = "theme.mode"
    }
}
