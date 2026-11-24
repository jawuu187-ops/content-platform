package com.contentplatform.app.dm

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.net.SocketTimeoutException

/**
 * Error mapping is the difference between "your membership ended" and "something went
 * wrong". Each case here corresponds to a branch in the thread UI, so a wrong mapping is a
 * wrong message to a person.
 */
class DmFailureTest {

    @Test
    fun aLapsedMembershipIsNotAGenericRefusal() {
        val failure = ApiException(403, "subscription_required", "a live subscription is required")
            .toDmFailure()
        assertEquals(DmFailure.SubscriptionRequired, failure)
        assertTrue(failure.explain().contains("membership"))
        assertTrue(failure.explain().contains("readable"))
    }

    @Test
    fun anExpiredTokenAsksForASignIn() {
        assertEquals(DmFailure.SignedOut, ApiException(401, "unauthenticated", "").toDmFailure())
        assertEquals(DmFailure.SignedOut, ApiException(401, "", "").toDmFailure())
    }

    @Test
    fun someoneElsesThreadIsRefusedWithoutPretendingItIsMissing() {
        val failure = ApiException(403, "not_a_participant", "this conversation is not yours")
            .toDmFailure()
        assertEquals(DmFailure.NotAllowed("this conversation is not yours"), failure)
    }

    @Test
    fun aMissingThreadIsNotFound() {
        assertEquals(DmFailure.NotFound, ApiException(404, "not_found", "").toDmFailure())
    }

    @Test
    fun aRejectedBodyIsReportedAsInvalid() {
        val failure = ApiException(400, "invalid_message", "message body is empty").toDmFailure()
        assertTrue(failure is DmFailure.Invalid)
    }

    @Test
    fun networkTroubleIsOfflineNotAServerFault() {
        assertEquals(DmFailure.Offline, SocketTimeoutException("timeout").toDmFailure())
        assertEquals(DmFailure.Offline, IOException("connection reset").toDmFailure())
        assertTrue(DmFailure.Offline.explain().contains("offline"))
        assertTrue(DmFailure.Offline.explain().contains("still here"))
    }

    @Test
    fun aServerFaultNeverLeaksTheRawExceptionText() {
        val failure = ApiException(500, "internal_error", "").toDmFailure()
        assertTrue(failure is DmFailure.Server)
        val text = failure.explain()
        assertTrue(text.contains("Try again"))
        assertTrue(!text.contains("ApiException"))
    }
}
