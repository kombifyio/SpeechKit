import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    `maven-publish`
}

// The `speechkit.coinstall.v1` wire contract: AIDL plus the constants that
// name the service. Implemented by the Companion, called by this repository's
// keyboard/assistant APK.
//
// It deliberately contains no implementation and declares no dependency beyond
// the Kotlin stdlib. That is a licensing constraint, not a preference: the
// assembled `io.kombify.speechkit`
// APK is GPL-3.0 because it links the HeliBoard fork, the Companion is
// proprietary, and neither app may link the other's code. Both sides compiling
// the same Apache-2.0 contract artifact is the only seam that survives that
// boundary — the generated stubs carry code from neither app.
//
// Specification: docs/architecture/android-coinstall-contract.md.
android {
    namespace = "io.kombify.speechkit.coinstall.v1"
    compileSdk = 36
    defaultConfig { minSdk = 26 }
    buildFeatures { aidl = true }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    // Without this list AGP compiles the AIDL into the AAR's classes.jar but
    // ships no `aidl/` folder, so a consumer can call the stubs yet cannot
    // `import` these types from an `.aidl` of its own. A contract artifact that
    // withholds the contract source is half an artifact; every type is listed
    // because the contract is the whole module.
    aidlPackagedList(
        "io/kombify/speechkit/coinstall/v1/CapabilityStatus.aidl",
        "io/kombify/speechkit/coinstall/v1/ICoinstallCallback.aidl",
        "io/kombify/speechkit/coinstall/v1/ICoinstallService.aidl",
        "io/kombify/speechkit/coinstall/v1/ProvisionRequest.aidl",
        "io/kombify/speechkit/coinstall/v1/ProvisionResult.aidl",
        "io/kombify/speechkit/coinstall/v1/TurnRequest.aidl",
    )

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }
}

// Published as `coinstall-contract`, not `coinstall`: the artifactId is what a
// consumer reads in its dependency block, and "contract" is the load-bearing
// word — an app depending on this must not expect an implementation. The name
// follows owner decision 2026-08-11 A8, which both this repository and
// kombify-Mobile pin against.
publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.kombify.speechkit"
            artifactId = "coinstall-contract"
            version = providers.fileContents(
                rootProject.layout.projectDirectory.file("../.kombify/VERSION"),
            ).asText.map { it.trim() }.getOrElse("0.0.0")

            afterEvaluate { from(components["release"]) }

            pom {
                name.set("SpeechKit Co-install Contract")
                description.set(
                    "The speechkit.coinstall.v1 AIDL contract between the SpeechKit " +
                        "keyboard/assistant APK and the kombify Companion. Contract only: " +
                        "the wire surface and its constants, no implementation.",
                )
                licenses {
                    license {
                        name.set("Apache-2.0")
                        url.set("https://www.apache.org/licenses/LICENSE-2.0")
                    }
                }
            }
        }
    }

    repositories {
        maven {
            name = "GitHubPackages"
            url = uri("https://maven.pkg.github.com/kombifyio/SpeechKit")
            credentials {
                username = providers.gradleProperty("gpr.user")
                    .orElse(providers.environmentVariable("GITHUB_ACTOR")).getOrElse("")
                password = providers.gradleProperty("gpr.token")
                    .orElse(providers.environmentVariable("GITHUB_TOKEN")).getOrElse("")
            }
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
