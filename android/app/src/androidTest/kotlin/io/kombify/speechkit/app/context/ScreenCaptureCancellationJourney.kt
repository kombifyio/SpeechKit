package io.kombify.speechkit.app.context

import android.os.SystemClock
import android.view.KeyEvent
import androidx.test.platform.app.InstrumentationRegistry
import io.kombify.speechkit.app.context.capture.ScreenCaptureActivity
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Drives the real system consent dialog; never grants projection. */
class ScreenCaptureCancellationJourney {
    @Test fun decliningSystemConsentReturnsNoImage() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Screen capture requires an explicit interactive device run", args.getString("requireContextCapture") == "true")
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val completed = CountDownLatch(1)
        val result = AtomicReference<Result<android.graphics.Bitmap>>()
        instrumentation.runOnMainSync {
            assertTrue(ScreenCaptureActivity.request(context) { result.set(it); completed.countDown() })
        }
        try {
            val deadline = SystemClock.uptimeMillis() + 10_000
            var dialogVisible = false
            while (SystemClock.uptimeMillis() < deadline) {
                val packageName = instrumentation.uiAutomation.rootInActiveWindow?.packageName?.toString()
                if (packageName != null && (packageName.contains("systemui") || packageName.contains("permissioncontroller"))) { dialogVisible = true; break }
                SystemClock.sleep(100)
            }
            assertTrue("System projection consent did not appear", dialogVisible)
            instrumentation.sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
            assertTrue("Declining projection must complete the request", completed.await(10, TimeUnit.SECONDS))
            assertFalse("Declining projection must never return pixels", requireNotNull(result.get()).isSuccess)
        } finally {
            instrumentation.runOnMainSync { ScreenCaptureActivity.cancel(context) }
        }
    }
}
