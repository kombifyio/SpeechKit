package io.kombify.speechkit.app.companion

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ConnectCloudIntentTest {

    @Test
    fun speechkitConnectUriFinishesCloudConnect() {
        assertTrue(ConnectCloudIntent.matches("speechkit", "connect", "/kombify"))
        assertTrue(ConnectCloudIntent.matches("speechkit", "connect", "kombify"))
        assertTrue(ConnectCloudIntent.matches("speechkit", "connect", "/"))
        assertTrue(ConnectCloudIntent.matches("https", "speechkit.kombify.io", "/connect"))
    }

    @Test
    fun otherUrisDoNotFinishCloudConnect() {
        assertFalse(ConnectCloudIntent.matches("speechkit", "settings", "/kombify"))
        assertFalse(ConnectCloudIntent.matches("https", "example.com", "/connect"))
        assertFalse(ConnectCloudIntent.matches(null, null, null))
    }
}
