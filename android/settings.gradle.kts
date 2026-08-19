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
include(":app")
include(":test-shared")

// The HeliBoard fork's keyboard module, consumed from the submodule as a
// library. Its own root build script is not evaluated here -- plugin versions
// come from this build, where the Kotlin plugin may only appear once.
//
// GPL-3.0. Everything that links this module is distributed under GPL-3.0 as a
// whole, so no proprietary Kombify code may enter that APK. See
// android/HELIBOARD.md.
include(":heliboard")
project(":heliboard").projectDir = file("heliboard/app")
