package com.contentplatform.app.dm

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The report payload is rendered straight from the server, so a change in its shape must
 * fail here rather than produce a blank screen.
 */
class ModerationModelsTest {

    private fun reportJson(status: String = "reviewing"): JSONObject = JSONObject(
        """
        {
          "id": "6f1c6f0e-1111-2222-3333-444455556666",
          "reporter_id": "aaaa1111-2222-3333-4444-555566667777",
          "subject_type": "message",
          "subject_id": "bbbb1111-2222-3333-4444-555566667777",
          "reason": "harassment",
          "details": "repeated threats",
          "status": "$status",
          "created_at": "2026-11-26T10:00:00Z",
          "updated_at": "2026-11-26T11:00:00Z",
          "resolved_at": null,
          "events": [
            {"id": 1, "action": "", "from_status": "", "to_status": "open", "note": "", "created_at": "2026-11-26T10:00:00Z"},
            {"id": 2, "action": "start_review", "from_status": "open", "to_status": "reviewing", "note": "looking", "created_at": "2026-11-26T11:00:00Z"}
          ]
        }
        """.trimIndent(),
    )

    @Test
    fun reportKeepsItsHistoryInOrder() {
        val report = parseReport(reportJson())!!
        assertEquals("harassment", report.reason)
        assertEquals("reviewing", report.status)
        assertEquals(2, report.events.size)
        assertEquals("open", report.events[0].toStatus)
        assertEquals("start_review", report.events[1].action)
        assertEquals("looking", report.events[1].note)
        assertFalse(report.duplicate)
        assertTrue(report.createdAtMillis != null)
    }

    @Test
    fun aClosedReportIsClosed() {
        val closed = parseReport(reportJson("dismissed"))!!
        assertTrue(closed.closed)
        val open = parseReport(reportJson("open"))!!
        assertFalse(open.closed)
    }

    @Test
    fun malformedReportsAreNotInvented() {
        assertNull(parseReport(null))
        assertNull(parseReport(JSONObject("""{"reason":"spam"}""")))
    }

    @Test
    fun everyAcceptedReasonHasPlainWords() {
        assertEquals(7, ModerationReason.codes.size)
        ModerationReason.codes.forEach { code ->
            val label = ModerationReason.label(code)
            assertTrue("reason $code has no label", label.isNotBlank())
            assertFalse("reason $code leaked its code", label == code)
        }
    }
}
