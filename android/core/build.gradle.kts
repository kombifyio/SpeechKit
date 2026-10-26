import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    `maven-publish`
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "io.kombify.speechkit.core"
    compileSdk = 36

    defaultConfig {
        minSdk = 26
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        consumerProguardFiles("consumer-rules.pro")
    }

    // The NDK's clang compiles the Phonon engine (buildPhononNative below); same version as the app module's.
    ndkVersion = "28.0.13004108"

    sourceSets["main"].jniLibs.srcDir(layout.buildDirectory.dir("phonon-jni"))

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    publishing {
        singleVariant("release") {
            withSourcesJar()
        }
    }

}

// The Phonon engine, libspeechkit_phonon.so: SpeechKit's five-value engine and its Go model loader
// (app/cmd/speechkit-phonon-jni), built with Go and the NDK's clang for the 64-bit ABIs the engine supports. A
// machine without Go builds the module without it; PhononNative.available() is then false and the app offers no
// Phonon model. SPEECHKIT_GO names the go binary when it is not on PATH.
val buildPhononNative by tasks.registering {
    description = "Builds libspeechkit_phonon.so for arm64-v8a and x86_64 with Go and the NDK"
    val abis = mapOf("arm64-v8a" to ("arm64" to "aarch64-linux-android"), "x86_64" to ("amd64" to "x86_64-linux-android"))
    val repoRoot = rootProject.projectDir.parentFile
    val outDir = layout.buildDirectory.dir("phonon-jni")
    val windows = System.getProperty("os.name").lowercase().contains("windows")
    // Everything the library compiles from: the engine, the loader and the sherpa types it uses, the JNI command,
    // and the module files that pin their dependencies.
    inputs.dir(repoRoot.resolve("pkg/speechkit/x/stt/phonon"))
    inputs.dir(repoRoot.resolve("pkg/speechkit/x/stt/sherpa"))
    inputs.dir(repoRoot.resolve("app/cmd/speechkit-phonon-jni"))
    inputs.files(
        listOf("go.mod", "go.sum", "go.work", "app/go.mod", "app/go.sum", "pkg/speechkit/x/go.mod", "pkg/speechkit/x/go.sum")
            .map(repoRoot::resolve),
    )
    outputs.dir(outDir)
    // A run without Go leaves no library; it is not up to date, so the next run with Go builds it.
    outputs.upToDateWhen { abis.keys.all { outDir.get().file("$it/libspeechkit_phonon.so").asFile.isFile } }
    doLast {
        val go = System.getenv("SPEECHKIT_GO") ?: "go"
        val hasGo = runCatching { ProcessBuilder(go, "version").start().waitFor() == 0 }.getOrDefault(false)
        if (!hasGo) {
            logger.warn("Go not found: building without the Phonon engine (libspeechkit_phonon.so)")
            return@doLast
        }
        val ndkDir = android.ndkDirectory
        val toolchain = ndkDir.resolve("toolchains/llvm/prebuilt").listFiles()?.firstOrNull()
            ?: throw GradleException("NDK toolchain not found under $ndkDir")
        for ((abi, target) in abis) {
            val (goarch, triple) = target
            val clang = toolchain.resolve("bin/${triple}26-clang" + if (windows) ".cmd" else "")
            val out = outDir.get().file("$abi/libspeechkit_phonon.so").asFile
            out.parentFile.mkdirs()
            val process = ProcessBuilder(
                go, "build", "-buildmode=c-shared", "-trimpath", "-ldflags=-s -w", "-o", out.absolutePath,
                "./app/cmd/speechkit-phonon-jni",
            ).directory(repoRoot).inheritIO().apply {
                environment().putAll(
                    mapOf(
                        "GOOS" to "android", "GOARCH" to goarch, "CGO_ENABLED" to "1",
                        // cgo splits CC at spaces: the NDK must sit at a path without them.
                        "CC" to clang.absolutePath,
                        "GOFLAGS" to listOfNotNull(System.getenv("GOFLAGS"), "-buildvcs=false").joinToString(" "),
                    ),
                )
            }.start()
            if (process.waitFor() != 0) throw GradleException("building libspeechkit_phonon.so for $abi failed")
            // c-shared also writes a C header; only the library ships.
            out.resolveSibling("libspeechkit_phonon.h").delete()
        }
    }
}
tasks.named("preBuild") { dependsOn(buildPhononNative) }

// Published because docs/architecture/android-sdk-surface-boundary.md names
// this module part of the Android embedder boundary. A module a host is told
// to depend on but cannot resolve is a documented capability that does not
// exist; vendoring a copy is what that gap produced last time.
publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.kombify.speechkit"
            artifactId = "core"
            version = providers.fileContents(
                rootProject.layout.projectDirectory.file("../.kombify/VERSION"),
            ).asText.map { it.trim() }.getOrElse("0.0.0")

            afterEvaluate { from(components["release"]) }

            pom {
                name.set("SpeechKit Core (Android)")
                description.set(
                    "Microphone capture, PCM playback, the speech engine, VAD and the turn engine shared by every SpeechKit voice surface.",
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
    // Kotlin & Coroutines
    implementation(libs.kotlin.stdlib)
    implementation(libs.coroutines.core)
    implementation(libs.coroutines.android)

    // ONNX Runtime (Silero VAD + on-device Whisper)
    implementation(libs.onnxruntime)

    // View-tree owners for service-hosted windows (ui/ComposeInServiceWindow.kt)
    implementation(libs.lifecycle.runtime)
    implementation(libs.lifecycle.viewmodel)
    implementation(libs.savedstate.ktx)

    // Networking (HuggingFace / VPS HTTP providers)
    implementation(libs.okhttp)
    implementation(libs.okhttp.logging)
    implementation(libs.retrofit)
    implementation(libs.retrofit.moshi)
    implementation(libs.moshi)
    ksp(libs.moshi.codegen)

    // Room (SQLite store)
    implementation(libs.room.runtime)
    implementation(libs.room.ktx)
    ksp(libs.room.compiler)

    // DI
    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)

    // Logging
    implementation(libs.timber)

    // Testing
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.espresso.core)
    androidTestImplementation(libs.room.testing)
}

// Kotlin 2.4 turned the kotlinOptions DSL from deprecated into an error.
kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
