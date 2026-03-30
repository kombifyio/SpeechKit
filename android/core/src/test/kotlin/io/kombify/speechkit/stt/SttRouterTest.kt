package io.kombify.speechkit.stt

import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import kotlin.time.Duration.Companion.seconds

class SttRouterTest {

    private val opts = TranscribeOpts(language = "de")

    private fun fakeProvider(name: String, text: String = "test"): SttProvider = object : SttProvider {
        override val name: String = name
        override suspend fun transcribe(audio: ByteArray, opts: TranscribeOpts) = Result(
            text = text, language = opts.language, duration = 1.seconds,
            provider = name, model = "test-model", confidence = 0.95,
        )
        override suspend fun health() {}
    }

    private fun failingProvider(name: String): SttProvider = object : SttProvider {
        override val name: String = name
        override suspend fun transcribe(audio: ByteArray, opts: TranscribeOpts): Result =
            throw RuntimeException("$name failed")
        override suspend fun health() { throw RuntimeException("$name unhealthy") }
    }

    @Test
    fun `local-only strategy uses local provider`() = runTest {
        val router = SttRouter(
            local = fakeProvider("local", "local-result"),
            strategy = SttRouter.RoutingStrategy.LOCAL_ONLY,
        )

        val result = router.route(ByteArray(1000), 1.0, opts)
        assertEquals("local-result", result.text)
        assertEquals("local", result.provider)
    }

    @Test
    fun `local-only throws when no local provider`() = runTest {
        val router = SttRouter(strategy = SttRouter.RoutingStrategy.LOCAL_ONLY)

        assertThrows<IllegalStateException> {
            router.route(ByteArray(1000), 1.0, opts)
        }
    }

    @Test
    fun `cloud-only strategy uses cloud provider`() = runTest {
        val router = SttRouter(
            cloud = mutableListOf(fakeProvider("hf", "cloud-result")),
            strategy = SttRouter.RoutingStrategy.CLOUD_ONLY,
        )

        val result = router.route(ByteArray(1000), 1.0, opts)
        assertEquals("cloud-result", result.text)
        assertEquals("hf", result.provider)
    }

    @Test
    fun `cloud-only falls through ordered providers`() = runTest {
        val router = SttRouter(
            cloud = mutableListOf(
                failingProvider("vps"),
                fakeProvider("hf", "hf-result"),
            ),
            strategy = SttRouter.RoutingStrategy.CLOUD_ONLY,
        )

        val result = router.route(ByteArray(1000), 1.0, opts)
        assertEquals("hf-result", result.text)
    }

    @Test
    fun `dynamic prefers local for short audio`() = runTest {
        val router = SttRouter(
            local = fakeProvider("local", "local-result"),
            cloud = mutableListOf(fakeProvider("hf", "cloud-result")),
            strategy = SttRouter.RoutingStrategy.DYNAMIC,
            preferLocalUnderSecs = 10.0,
            connectivityCheck = { true },
        )

        val result = router.route(ByteArray(1000), 5.0, opts)
        assertEquals("local-result", result.text)
    }

    @Test
    fun `dynamic prefers cloud for long audio`() = runTest {
        val router = SttRouter(
            local = fakeProvider("local", "local-result"),
            cloud = mutableListOf(fakeProvider("hf", "cloud-result")),
            strategy = SttRouter.RoutingStrategy.DYNAMIC,
            preferLocalUnderSecs = 10.0,
            connectivityCheck = { true },
        )

        val result = router.route(ByteArray(1000), 15.0, opts)
        assertEquals("cloud-result", result.text)
    }

    @Test
    fun `dynamic falls back to local when offline`() = runTest {
        val router = SttRouter(
            local = fakeProvider("local", "offline-result"),
            cloud = mutableListOf(fakeProvider("hf", "cloud-result")),
            strategy = SttRouter.RoutingStrategy.DYNAMIC,
            connectivityCheck = { false },
        )

        val result = router.route(ByteArray(1000), 15.0, opts)
        assertEquals("offline-result", result.text)
    }

    @Test
    fun `dynamic throws when offline and no local`() = runTest {
        val router = SttRouter(
            strategy = SttRouter.RoutingStrategy.DYNAMIC,
            connectivityCheck = { false },
        )

        assertThrows<IllegalStateException> {
            router.route(ByteArray(1000), 5.0, opts)
        }
    }

    @Test
    fun `dynamic falls back to local when cloud fails`() = runTest {
        val router = SttRouter(
            local = fakeProvider("local", "fallback-result"),
            cloud = mutableListOf(failingProvider("hf")),
            strategy = SttRouter.RoutingStrategy.DYNAMIC,
            preferLocalUnderSecs = 10.0,
            connectivityCheck = { true },
        )

        // Long audio -> tries cloud first -> fails -> falls back to local
        val result = router.route(ByteArray(1000), 15.0, opts)
        assertEquals("fallback-result", result.text)
    }

    @Test
    fun `availableProviders lists all configured`() {
        val router = SttRouter(
            local = fakeProvider("local"),
            cloud = mutableListOf(fakeProvider("vps"), fakeProvider("hf")),
        )

        val providers = router.availableProviders()
        assertEquals(listOf("local", "vps", "hf"), providers)
    }

    @Test
    fun `setCloud replaces provider by name`() {
        val router = SttRouter(
            cloud = mutableListOf(fakeProvider("hf", "old")),
        )

        router.setCloud("hf", fakeProvider("hf", "new"))
        assertEquals(1, router.availableProviders().count { it == "hf" })
    }

    @Test
    fun `setCloud with null removes provider`() {
        val router = SttRouter(
            cloud = mutableListOf(fakeProvider("hf"), fakeProvider("vps")),
        )

        router.setCloud("hf", null)
        assertEquals(listOf("vps"), router.availableProviders())
    }
}
