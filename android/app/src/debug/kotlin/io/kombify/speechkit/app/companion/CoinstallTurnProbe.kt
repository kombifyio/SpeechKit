package io.kombify.speechkit.app.companion

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.assistant.intent.CompanionTurnExecutor
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * Development builds only: the Android journey's way to hand one general query to the exact executor the
 * system assistant uses after speech recognition (`AssistantListenTurn`), without driving a microphone.
 * Only the shell can send it (`android.permission.DUMP`); it logs the gate, route and outcome, never text.
 *
 * `adb shell am broadcast -n io.kombify.speechkit/.app.companion.CoinstallTurnProbe --es text "<question>"`
 * logs (`adb logcat -s sk.voice`), after the client's own `assist companion turn outcome=… first_ms=…` line:
 * `assist coinstall probe available=<bool> route=companion|local success=<bool> answer_chars=<n>`.
 * With `--ez gate_only true` it asks only the route gate and logs `assist coinstall probe gate available=<bool>`.
 */
class CoinstallTurnProbe : BroadcastReceiver() {
    @EntryPoint
    @InstallIn(SingletonComponent::class)
    interface ProbeEntryPoint {
        fun companionTurnExecutor(): CompanionTurnExecutor
    }

    override fun onReceive(context: Context, intent: Intent) {
        val text = intent.getStringExtra(EXTRA_TEXT)?.trim()?.take(MAX_CHARS).orEmpty().ifEmpty { DEFAULT_TEXT }
        val gateOnly = intent.getBooleanExtra(EXTRA_GATE_ONLY, false)
        val executor = EntryPointAccessors
            .fromApplication(context.applicationContext, ProbeEntryPoint::class.java)
            .companionTurnExecutor()
        val pending = goAsync()
        scope.launch {
            try {
                val available = executor.available()
                if (gateOnly) {
                    VoiceLog.i(VoiceLog.ASSIST, "coinstall probe gate available=$available")
                    return@launch
                }
                val answer = if (available) executor.startTurn(text) else null
                val route = if (answer != null) "companion" else "local"
                VoiceLog.i(
                    VoiceLog.ASSIST,
                    "coinstall probe available=$available route=$route success=${answer?.success == true} " +
                        "answer_chars=${answer?.responseText?.length ?: 0}",
                )
            } finally {
                pending.finish()
            }
        }
    }

    private companion object {
        const val EXTRA_TEXT = "text"
        const val EXTRA_GATE_ONLY = "gate_only"
        const val DEFAULT_TEXT = "What can you help me with today?"
        const val MAX_CHARS = 500
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    }
}
