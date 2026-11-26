package io.kombify.speechkit.domain

/** Destination permission is separate from the user's preferred execution location. */
enum class ExecutionMode { DEVICE_ONLY, PREFER_DEVICE, PREFER_ENDPOINT, ENDPOINT_ONLY }
enum class ExecutionLocation { DEVICE, ENDPOINT }
data class ExecutionPolicy(val mode: ExecutionMode, val allowNetwork: Boolean = false) {
    fun candidates(deviceAvailable: Boolean, endpointAvailable: Boolean): List<ExecutionLocation> {
        val device = listOfNotNull(ExecutionLocation.DEVICE.takeIf { deviceAvailable })
        val endpoint = listOfNotNull(ExecutionLocation.ENDPOINT.takeIf {
            endpointAvailable && allowNetwork && mode != ExecutionMode.DEVICE_ONLY
        })
        return when (mode) {
            ExecutionMode.DEVICE_ONLY -> device
            ExecutionMode.PREFER_DEVICE -> device + endpoint
            ExecutionMode.PREFER_ENDPOINT -> endpoint + device
            ExecutionMode.ENDPOINT_ONLY -> endpoint
        }
    }

    companion object {
        /** Retain an existing explicit endpoint choice; a fresh install stays local. */
        fun fromLegacy(profile: ConnectionProfile): ExecutionPolicy = when (profile) {
            is ConnectionProfile.Server, is ConnectionProfile.Byok, is ConnectionProfile.Firebase -> ExecutionPolicy(ExecutionMode.PREFER_ENDPOINT, true)
            else -> ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
        }
    }
}

/** A gesture changes only this invocation; exclusive modes keep their destination. */
fun ExecutionPolicy.forInvocation(alternative: Boolean): ExecutionPolicy = if (!alternative) this else when (mode) {
    ExecutionMode.PREFER_ENDPOINT -> ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
    ExecutionMode.PREFER_DEVICE -> ExecutionPolicy(ExecutionMode.ENDPOINT_ONLY, allowNetwork)
    ExecutionMode.DEVICE_ONLY, ExecutionMode.ENDPOINT_ONLY -> this
}

/** Per-call routing may narrow permission, never undo an exclusive user choice. */
fun ExecutionPolicy.restrictTo(override: ExecutionPolicy?): ExecutionPolicy {
    if (override == null) return this
    require(mode !in setOf(ExecutionMode.DEVICE_ONLY, ExecutionMode.ENDPOINT_ONLY) || override.mode == mode) { "Execution mode changed" }
    return override.copy(allowNetwork = allowNetwork && override.allowNetwork && override.mode != ExecutionMode.DEVICE_ONLY)
}

/** Only explicitly classified, safe failures may trigger fallback. */
enum class ExecutionFailureKind { UNAVAILABLE, TRANSIENT, AUTHORIZATION, POLICY_DENIED, SAFETY, INTERRUPTED, OTHER }
class ExecutionFailure(val kind: ExecutionFailureKind, message: String, cause: Throwable? = null) :
    Exception(message, cause)

data class ExecutionResult<T>(val value: T, val location: ExecutionLocation, val fallbackReason: ExecutionFailureKind? = null,
    val adapterId: String? = null)

/** For side-effect-free work only. Dispatched agent turns must never be replayed here. */
suspend fun <T> executeGeneration(
    policy: ExecutionPolicy,
    device: (suspend () -> T)?,
    endpoint: (suspend () -> T)?,
): ExecutionResult<T> {
    val candidates = policy.candidates(device != null, endpoint != null)
    if (candidates.isEmpty()) throw ExecutionFailure(ExecutionFailureKind.UNAVAILABLE, "No permitted capability available")
    var previous: ExecutionFailure? = null
    val preferredUnavailable = when (policy.mode) {
        ExecutionMode.PREFER_DEVICE -> device == null
        ExecutionMode.PREFER_ENDPOINT, ExecutionMode.ENDPOINT_ONLY -> endpoint == null || !policy.allowNetwork
        ExecutionMode.DEVICE_ONLY -> false
    }
    for ((index, location) in candidates.withIndex()) {
        try {
            val value = when (location) {
                ExecutionLocation.DEVICE -> device!!.invoke()
                ExecutionLocation.ENDPOINT -> endpoint!!.invoke()
            }
            return ExecutionResult(value, location, previous?.kind ?: ExecutionFailureKind.UNAVAILABLE.takeIf { preferredUnavailable })
        } catch (failure: ExecutionFailure) {
            if (failure.kind !in setOf(ExecutionFailureKind.UNAVAILABLE, ExecutionFailureKind.TRANSIENT) ||
                index == candidates.lastIndex
            ) throw failure
            previous = failure
        }
    }
    throw previous ?: ExecutionFailure(ExecutionFailureKind.UNAVAILABLE, "No permitted capability available")
}

