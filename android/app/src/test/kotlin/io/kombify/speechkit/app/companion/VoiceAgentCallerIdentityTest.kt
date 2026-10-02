package io.kombify.speechkit.app.companion

import android.content.pm.PackageManager
import io.mockk.every
import io.mockk.mockk
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class VoiceAgentCallerIdentityTest {
    @Test
    fun `only the current app signing authority can receive a hosted voice session`() {
        val packages = mockk<PackageManager>()
        val uid = 12001
        val play = "fa16ae10d7333a6006a4d93cd321e0593ba5b1d7010d2f3c91ed3e6e36bea4d3"
            .chunked(2).map { it.toInt(16).toByte() }.toByteArray()
        var validCertificate = true
        every { packages.getPackagesForUid(uid) } returns arrayOf("io.kombify.companion")
        every { packages.hasSigningCertificate(any<String>(), any<ByteArray>(), PackageManager.CERT_INPUT_SHA256) } answers {
            validCertificate && firstArg<String>() == "io.kombify.companion" && secondArg<ByteArray>().contentEquals(play)
        }
        val identity = VoiceAgentCallerIdentity(packages)
        assertTrue(identity.attested(uid))
        validCertificate = false
        assertFalse(identity.attested(uid))
        validCertificate = true
        every { packages.getPackagesForUid(uid) } returns arrayOf("io.kombify.companion", "untrusted.shared.uid")
        assertFalse(identity.attested(uid))
        every { packages.getPackagesForUid(uid) } returns arrayOf("io.kombify.ai")
        assertFalse(identity.attested(uid))
    }
}
