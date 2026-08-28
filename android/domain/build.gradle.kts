import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    `maven-publish`
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "io.kombify.speechkit.domain"
    compileSdk = 36

    defaultConfig {
        minSdk = 26
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }

    testOptions {
        unitTests.all { it.useJUnitPlatform() }
    }
}

// Published because docs/architecture/android-sdk-surface-boundary.md names
// this module part of the Android embedder boundary. A module a host is told
// to depend on but cannot resolve is a documented capability that does not
// exist; vendoring a copy is what that gap produced last time.
publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.kombify.speechkit"
            artifactId = "domain"
            version = providers.fileContents(
                rootProject.layout.projectDirectory.file("../.kombify/VERSION"),
            ).asText.map { it.trim() }.getOrElse("0.0.0")

            afterEvaluate { from(components["release"]) }

            pom {
                name.set("SpeechKit Domain (Android)")
                description.set(
                    "Connection algebra: how a host resolves which SpeechKit server to talk to, with no Android or transport dependency.",
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

dependencies {
    implementation(libs.kotlin.stdlib)

    testImplementation(libs.junit.api)
    testRuntimeOnly(libs.junit.engine)
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
