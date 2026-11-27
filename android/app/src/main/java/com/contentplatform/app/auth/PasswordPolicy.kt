package com.contentplatform.app.auth

/**
 * Mirrors the server's registration policy exactly: 12–256 UTF-8 bytes, length only.
 * The check runs before the request so a short password never becomes a confusing
 * "registration failed, then login failed" sequence.
 */
object PasswordPolicy {
    const val MinBytes = 12
    const val MaxBytes = 256

    fun problem(password: String): String? {
        val bytes = password.toByteArray(Charsets.UTF_8).size
        return when {
            bytes < MinBytes -> "Use at least $MinBytes characters for your password."
            bytes > MaxBytes -> "Use no more than $MaxBytes characters for your password."
            else -> null
        }
    }
}
