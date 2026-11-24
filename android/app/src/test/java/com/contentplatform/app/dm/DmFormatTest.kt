package com.contentplatform.app.dm

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/**
 * The messaging copy and validation are pure functions, so they are tested on the JVM.
 * These tests are the reason the inbox can show a defensible timestamp without an emulator.
 */
class DmFormatTest {

    private val now = Instant.parse("2026-11-26T12:00:00Z").toEpochMilli()

    private fun ago(minutes: Long): Long = now - minutes * 60_000

    @Test
    fun relativeTimeUsesMinutesHoursThenDays() {
        assertEquals("now", DmFormat.relativeTime(now, now - 30_000))
        assertEquals("1m", DmFormat.relativeTime(now, ago(1)))
        assertEquals("59m", DmFormat.relativeTime(now, ago(59)))
        assertEquals("1h", DmFormat.relativeTime(now, ago(60)))
        assertEquals("23h", DmFormat.relativeTime(now, ago(60 * 23)))
        assertEquals("1d", DmFormat.relativeTime(now, ago(60 * 24)))
        assertEquals("6d", DmFormat.relativeTime(now, ago(60 * 24 * 6)))
    }

    @Test
    fun relativeTimeFallsBackToADateAfterAWeek() {
        val stamp = DmFormat.relativeTime(now, ago(60 * 24 * 10))
        assertTrue("expected a day and month, got $stamp", Regex("""^\d{1,2} [A-Z][a-z]{2}$""").matches(stamp))
    }

    @Test
    fun aClockThatIsBehindTheServerNeverShowsANegativeAge() {
        assertEquals("now", DmFormat.relativeTime(now, now + 5 * 60_000))
    }

    @Test
    fun threadTimeShowsTheClockTodayAndTheDateBefore() {
        val today = DmFormat.threadTime(now, ago(5))
        assertTrue("today should carry a clock time, got $today", today.contains(":"))

        val older = DmFormat.threadTime(now, ago(60 * 24 * 4))
        assertFalse("an older message should carry a date, got $older", older.contains(":"))
        assertTrue(older.contains("2026"))
    }

    @Test
    fun relativeTimeIsZoneIndependentUnderAWeek() {
        // Buckets under seven days come from elapsed millis, not a calendar, so a device in
        // any time zone must produce the same short label.
        assertEquals("3h", DmFormat.relativeTime(now, ago(3 * 60)))
    }

    @Test
    fun initialsTakeAtMostTwoLetters() {
        assertEquals("AL", DmFormat.initials("Ada Lovelace"))
        assertEquals("A", DmFormat.initials("Ada"))
        assertEquals("?", DmFormat.initials("   "))
        assertEquals("AB", DmFormat.initials("  ada   byron  "))
    }

    @Test
    fun draftValidationMatchesTheServerRules() {
        assertEquals(DraftCheck.Empty, checkDraft(""))
        assertEquals(DraftCheck.Empty, checkDraft("   \n  "))
        assertEquals(DraftCheck.Ok, checkDraft("hello"))
        assertEquals(DraftCheck.Ok, checkDraft("  hello  "))
        assertEquals(DraftCheck.TooLong, checkDraft("x".repeat(DmFormat.MaxLength + 1)))
        assertEquals(DraftCheck.Ok, checkDraft("x".repeat(DmFormat.MaxLength)))
    }

    @Test
    fun lengthCountsTheTrimmedText() {
        assertEquals(5, DmFormat.length("  hello  "))
        assertEquals(0, DmFormat.length("   "))
        assertTrue(DmFormat.canSend("hi"))
        assertFalse(DmFormat.canSend("   "))
    }

    @Test
    fun moneyIsFormattedFromMinorUnits() {
        assertEquals("\$12.00", money(1200))
        assertEquals("\$0.99", money(99))
        assertEquals("\$1000.00", money(100_000))
        assertEquals("-\$2.50", money(-250))
        assertEquals("\u20ac3.00", money(300, "EUR"))
        assertEquals("7.00 SEKR", money(700, "sekr"))
    }

    @Test
    fun typedAmountsBecomeWholeMinorUnits() {
        assertEquals(1500L, parseAmountToMinor("15"))
        assertEquals(1500L, parseAmountToMinor("15.00"))
        assertEquals(1550L, parseAmountToMinor("$15.50"))
        assertEquals(1500L, parseAmountToMinor(" 15.00 "))
        // Rounding down never charges more than the person typed.
        assertEquals(1599L, parseAmountToMinor("15.999"))
        assertEquals(null, parseAmountToMinor(""))
        assertEquals(null, parseAmountToMinor("abc"))
        assertEquals(null, parseAmountToMinor("-5"))
    }

    @Test
    fun tipBoundsMatchTheServer() {
        assertEquals(TipCheck.BelowMinimum, checkTip(0))
        assertEquals(TipCheck.BelowMinimum, checkTip(99))
        assertEquals(TipCheck.Ok, checkTip(MinTipMinor))
        assertEquals(TipCheck.Ok, checkTip(MaxTipMinor))
        assertEquals(TipCheck.AboveMaximum, checkTip(MaxTipMinor + 1))
        assertEquals(280, MaxTipNote)
    }

    @Test
    fun theThreadNoteDoesNotClaimEndToEndEncryption() {
        val note = DmFormat.NO_E2EE_NOTE.lowercase()
        assertTrue(note.contains("stored on our servers"))
        assertTrue(note.contains("does not use end-to-end encryption"))
        // A phrase that would read as a security claim must never appear.
        assertFalse(note.contains("encrypted end-to-end"))
        assertFalse(note.contains("private by default"))
    }
}
