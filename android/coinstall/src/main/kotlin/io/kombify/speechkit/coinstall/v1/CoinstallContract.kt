package io.kombify.speechkit.coinstall.v1

/** Wire constants for `speechkit.coinstall.v1`. Both apps compile this. */
object CoinstallContract {
    const val VERSION: Int = 1

    /** The permanent Companion app id (release channel); the Play listing a missing Companion is offered from. */
    const val COMPANION_RELEASE_PACKAGE: String = "io.kombify.companion"

    /** Companion app ids that implement the service, in the order a caller prefers them (release, preview). */
    val COMPANION_PACKAGES: List<String> = listOf(COMPANION_RELEASE_PACKAGE, "io.kombify.companion.preview")

    const val BIND_ACTION: String = "io.kombify.speechkit.coinstall.v1.BIND"

    /**
     * The permission a caller must hold to bind a Companion's service: `<companion application id>` plus
     * this suffix, declared by that Companion and requested by the caller.
     */
    const val BIND_PERMISSION_SUFFIX: String = ".permission.BIND_COINSTALL"

    /** `onError` codes the Companion adds to the caller's own 1 (rejected) to 3 (unavailable). */
    const val ERROR_SIGNED_OUT: Int = 10
    const val ERROR_TURN_FAILED: Int = 11
    const val ERROR_BAD_REQUEST: Int = 12
    const val ERROR_BUSY: Int = 13
}
