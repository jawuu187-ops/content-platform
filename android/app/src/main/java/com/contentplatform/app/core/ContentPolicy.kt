package com.contentplatform.app.core

/**
 * The single place where the platform's content rules are expressed in the
 * client. The server enforces the same rules; this copy exists so the UI never
 * renders something the server would refuse, and so the rules are unit-testable
 * without Android.
 */
object ContentPolicy {

    const val MINIMUM_AGE: Int = 18

    /** Where a piece of content is being shown. */
    enum class Surface { PUBLIC, PAID }

    /** How a piece of content was classified by moderation. */
    enum class Classification { SAFE, SUGGESTIVE, NUDITY, EXPLICIT, PROHIBITED }

    /**
     * Decides whether content may be shown.
     *
     * Public surfaces never show nudity or suggestion. Paid surfaces allow
     * non-explicit nudity and suggestion, but only for age-verified users who
     * hold the entitlement. Explicit and prohibited content is never shown,
     * whatever the subscription state.
     */
    fun isVisible(
        surface: Surface,
        classification: Classification,
        ageVerified: Boolean,
        entitled: Boolean,
    ): Boolean = when (classification) {
        Classification.EXPLICIT, Classification.PROHIBITED -> false
        Classification.SAFE -> when (surface) {
            Surface.PUBLIC -> true
            Surface.PAID -> ageVerified && entitled
        }
        Classification.SUGGESTIVE, Classification.NUDITY -> when (surface) {
            Surface.PUBLIC -> false
            Surface.PAID -> ageVerified && entitled
        }
    }
}
