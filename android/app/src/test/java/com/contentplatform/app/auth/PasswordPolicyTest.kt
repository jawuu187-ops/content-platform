package com.contentplatform.app.auth

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class PasswordPolicyTest {
    @Test
    fun rejectsTheShortPasswordThatCausedRegistrationToFail() {
        assertNotNull(PasswordPolicy.problem("short"))
        assertNotNull(PasswordPolicy.problem("elevenchars"))
    }

    @Test
    fun acceptsTheServerMinimumAndLongerPasswords() {
        assertNull(PasswordPolicy.problem("twelve chars"))
        assertNull(PasswordPolicy.problem("a-very-long-password"))
    }

    @Test
    fun countsUtf8BytesLikeTheServer() {
        // Four three-byte characters are 12 bytes, so the server accepts them even
        // though a character count would say four.
        assertNull(PasswordPolicy.problem("\u20ac\u20ac\u20ac\u20ac"))
        assertEquals("Use at least 12 characters for your password.", PasswordPolicy.problem("abc"))
    }

    @Test
    fun rejectsPasswordsBeyondTheServerMaximum() {
        assertNotNull(PasswordPolicy.problem("x".repeat(PasswordPolicy.MaxBytes + 1)))
    }
}
