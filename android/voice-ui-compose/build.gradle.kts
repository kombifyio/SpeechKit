import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    `maven-publish`
}

// The shared Compose voice UI: one implementation of the assistant orb for
// every Android surface that draws it — the assistant overlay, the keyboard
// panel, and (through the Companion's vendored snapshot) the watch.
//
// It deliberately depends on nothing but Compose. A surface must be able to
// draw the orb without pulling in a session, a network client, or dependency
// injection, which is what kept the keyboard from reusing it before: the orb
// lived inside the assistant module, and no keyboard should depend on an
// assistant application to render a circle.
//
// The visual language itself is specified in
// clients/typescript/packages/voice-ui/src/tokens/tokens.json (the
// `assistant` block). This module implements those tokens; a change here
// without a change there is drift.
android {
    namespace = "io.kombify.speechkit.voiceui"
    compileSdk = 36

    defaultConfig {
        minSdk = 26
    }

    buildFeatures {
        compose = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    testOptions {
        // JUnit 5 (Jupiter) tests silently do not run without this.
        unitTests.all { it.useJUnitPlatform() }
    }

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }
}

// Published so surfaces outside this repository can draw the same orb —
// specifically the Companion's Wear app, which today carries a vendored copy
// with no upstream source. A shared visual that can only be consumed by
// copying is how the three divergent orbs happened in the first place.
publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.kombify.speechkit"
            artifactId = "voice-ui-compose"
            version = providers.fileContents(
                rootProject.layout.projectDirectory.file("../.kombify/VERSION"),
            ).asText.map { it.trim() }.getOrElse("0.0.0")

            afterEvaluate { from(components["release"]) }

            pom {
                name.set("SpeechKit Voice UI (Compose)")
                description.set(
                    "The canonical SpeechKit assistant orb for Compose surfaces. " +
                        "Brand-neutral: the host supplies its own mark.",
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
            url = uri("https://maven.pkg.github.com/KombiverseLabs/kombify-SpeechKit")
            credentials {
                username = providers.gradleProperty("gpr.user")
                    .orElse(providers.environmentVariable("GITHUB_ACTOR")).getOrElse("")
                password = providers.gradleProperty("gpr.token")
                    .orElse(providers.environmentVariable("GITHUB_TOKEN")).getOrElse("")
            }
        }
    }
}

dependencies {
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.foundation)
    implementation(libs.compose.runtime)

    testImplementation(libs.junit.api)
    testRuntimeOnly(libs.junit.engine)
}

// Kotlin 2.4 turned the kotlinOptions DSL from deprecated into an error.
kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
