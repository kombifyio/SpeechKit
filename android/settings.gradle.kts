pluginManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
        // The HeliBoard fork pulls colorpicker-compose from JitPack.
        maven { url = uri("https://jitpack.io") }
    }
}

rootProject.name = "speechkit-android"

include(":core")
include(":domain")
include(":voice-ui-compose")
include(":assistant")
include(":ime")
include(":net")
include(":coinstall")
include(":test-shared")

// The HeliBoard fork's keyboard module, consumed from the submodule as a
// library. Its own root build script is not evaluated here -- plugin versions
// come from this build, where the Kotlin plugin may only appear once.
//
// GPL-3.0. Everything that links this module is distributed under GPL-3.0 as a
// whole, so no proprietary Kombify code may enter that APK. See
// android/HELIBOARD.md.
//
// The include is conditional because the submodule is deliberately absent in
// two situations that both have to work: a fresh clone before
// `git submodule update --init`, and the public mirror, which records the
// revision in android/heliboard.rev instead of vendoring GPL sources. Without
// the guard, Gradle fails while evaluating settings, so neither case can even
// configure the Apache-2.0 framework modules — which is exactly what an
// outside consumer wants to build.
val heliboardDir = file("heliboard/app")
if (heliboardDir.isDirectory) {
    include(":heliboard")
    project(":heliboard").projectDir = heliboardDir
    // :app is the GPL assembly that links the fork, so it only exists when
    // the fork does.
    include(":app")
} else {
    logger.lifecycle(
        "settings: :heliboard is absent, so :app is excluded from this build. " +
            "Run `git submodule update --init android/heliboard` to build the reference APK.",
    )
}
