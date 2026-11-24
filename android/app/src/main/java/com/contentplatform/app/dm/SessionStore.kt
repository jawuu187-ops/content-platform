package com.contentplatform.app.dm

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue
import java.util.Locale
import java.util.UUID

/**
 * Where the app keeps the one thing it must not invent: who it is talking to the server as.
 *
 * The token is an ordinary access token from `POST /v1/auth/login`. It is kept in the app's
 * private SharedPreferences file, which is not encrypted at rest; on a rooted device that is
 * readable. That is an accepted limit of this build and it is recorded here rather than
 * hidden: a hardware-backed keystore wrap is the next step, not a claim already made.
 */
data class Session(
    val baseUrl: String,
    val accessToken: String,
    val email: String,
) {
    val signedIn: Boolean get() = baseUrl.isNotBlank() && accessToken.isNotBlank()
}

class SessionStore(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences("vesper.session", Context.MODE_PRIVATE)

    /**
     * Incremented whenever the stored session changes. The shell keys its permission read
     * on this, so signing in as a moderator during a session shows the safety queue instead
     * of hiding it until the next launch.
     */
    var revision by mutableIntStateOf(0)
        private set

    fun current(): Session = Session(
        baseUrl = prefs.getString(KEY_BASE_URL, "").orEmpty(),
        accessToken = prefs.getString(KEY_ACCESS_TOKEN, "").orEmpty(),
        email = prefs.getString(KEY_EMAIL, "").orEmpty(),
    )

    fun save(session: Session) {
        prefs.edit()
            .putString(KEY_BASE_URL, normalizeBaseUrl(session.baseUrl))
            .putString(KEY_ACCESS_TOKEN, session.accessToken)
            .putString(KEY_EMAIL, session.email)
            .apply()
        revision++
    }

    fun signOut() {
        prefs.edit()
            .remove(KEY_ACCESS_TOKEN)
            .apply()
        revision++
    }

    /** Stable per-install identifier used as the device fingerprint on login. */
    fun deviceFingerprint(): String {
        val existing = prefs.getString(KEY_FINGERPRINT, null)
        if (existing != null && existing.isNotBlank()) return existing
        val generated = UUID.randomUUID().toString()
        prefs.edit().putString(KEY_FINGERPRINT, generated).apply()
        return generated
    }

    companion object {
        private const val KEY_BASE_URL = "base_url"
        private const val KEY_ACCESS_TOKEN = "access_token"
        private const val KEY_EMAIL = "email"
        private const val KEY_FINGERPRINT = "device_fingerprint"

        /**
         * Accepts what a person actually types. A bare host gets https, a trailing slash is
         * dropped, and anything with a path is cut back to scheme://host:port so a pasted
         * deep link cannot quietly redirect every request.
         */
        fun normalizeBaseUrl(raw: String): String {
            var value = raw.trim()
            if (value.isEmpty()) return ""
            if (!value.startsWith("http://") && !value.startsWith("https://")) {
                value = "https://$value"
            }
            value = value.trimEnd('/')
            val schemeEnd = value.indexOf("://") + 3
            val pathStart = value.indexOf('/', startIndex = schemeEnd)
            return if (pathStart > 0) value.substring(0, pathStart) else value
        }

        fun platformName(): String = "android"

        fun deviceName(): String = "Android ${android.os.Build.VERSION.RELEASE}".lowercase(Locale.UK)
    }
}
