import java.net.HttpURLConnection
import java.net.URI
import java.security.MessageDigest
import java.io.FileInputStream
import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

/**
 * A build-time value from a Gradle property or an environment variable, empty
 * when neither is set. CI passes these for the tester lane; a developer build
 * gets nothing and falls back to the on-device tier.
 */
fun speechKitBuildValue(property: String, environment: String): String =
    (providers.gradleProperty(property).orNull ?: System.getenv(environment) ?: "")
        .trim()
        .replace("\\", "\\\\")
        .replace("\"", "\\\"")

android {
    namespace = "io.kombify.speechkit"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.kombify.speechkit"
        minSdk = 26
        targetSdk = 36
        versionCode = 7916
        versionName = "0.79.16"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        // An OSS installation has no admitted hosted media origin.
        buildConfigField("String", "HOSTED_VOICE_ORIGIN", "\"\"")
    }

    // Lint findings that predate the android-pr gate live in the baseline;
    // anything new fails the build. Fixing a baselined finding and running
    // `./gradlew :app:updateLintBaseline` shrinks it.
    lint {
        baseline = file("lint-baseline.xml")
    }

    // Release signing identity.
    //
    // This exists for speechkit.coinstall.v1: Companion pins the SHA-256 of
    // the certificate that signs THIS app and refuses any other caller. With
    // no release signingConfig, assembleRelease produced an unsigned APK and
    // every tester build fell back to a per-machine debug key, so no stable
    // fingerprint existed to pin and the cloud handshake could never succeed.
    //
    // The keystore is never committed: signing/keystore.properties is
    // gitignored and supplied locally or by CI. Without it the release build
    // falls back to debug signing so an ordinary checkout still builds — that
    // fallback is fine for compiling and wrong for distribution, which is why
    // the coinstall pin is what actually gates the cloud path.
    signingConfigs {
        create("release") {
            val propsFile = file("signing/keystore.properties")
            if (propsFile.exists()) {
                val props = Properties()
                FileInputStream(propsFile).use { props.load(it) }
                storeFile = file(props.getProperty("storeFile"))
                storePassword = props.getProperty("storePassword")
                keyAlias = props.getProperty("keyAlias")
                keyPassword = props.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = if (file("signing/keystore.properties").exists()) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
        }
    }

    flavorDimensions += "distribution"
    productFlavors {
        create("oss") {
            dimension = "distribution"
            applicationIdSuffix = ".oss"
            versionNameSuffix = "-oss"
        }
        create("kombify") {
            dimension = "distribution"
            // Distribution authority, never accepted from a Binder request.
            buildConfigField("String", "HOSTED_VOICE_ORIGIN",
                "\"" + speechKitBuildValue("speechkit.hostedVoiceOrigin", "SPEECHKIT_HOSTED_VOICE_ORIGIN") + "\"")

            // A connection the build can ship so a tester install exercises
            // every mode without pairing anything first. Both default to
            // empty: nothing is committed, the oss flavor never sees them,
            // and a developer build behaves exactly as before.
            //
            // Read this for what it is - a token inside an APK is not a
            // secret. Anyone holding the artifact can read it out, and
            // android/app/src ships as GPL corresponding source, so a value
            // hardcoded here would land in the public mirror as well. Pass
            // one only for a closed tester lane, keep it scoped and
            // revocable, and never set it for a public release.
            buildConfigField(
                "String",
                "DEFAULT_SERVER_URL",
                "\"" + speechKitBuildValue("speechkit.defaultServerUrl", "SPEECHKIT_DEFAULT_SERVER_URL") + "\"",
            )
            buildConfigField(
                "String",
                "DEFAULT_SERVER_TOKEN",
                "\"" + speechKitBuildValue("speechkit.defaultServerToken", "SPEECHKIT_DEFAULT_SERVER_TOKEN") + "\"",
            )
            // Public OAuth client id for the RFC 8628 device grant. Not a secret.
            buildConfigField(
                "String",
                "CLOUD_AUTH_CLIENT_ID",
                "\"" + speechKitBuildValue("speechkit.cloudAuthClientId", "SPEECHKIT_CLOUD_AUTH_CLIENT_ID") + "\"",
            )
        }
    }

    // Without an NDK the packaging step cannot find llvm-strip and logs
    // "Unable to strip library ... Packaging it as is" for every native
    // library -- ONNX Runtime and the keyboard's dictionary engine alike.
    // Matches the version the :heliboard module builds against.
    ndkVersion = "28.0.13004108"

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

}

// sherpa-onnx, the runtime of the on-device neural voices (io.kombify.speechkit.app.voice). The release's
// "static-link-onnxruntime" AAR carries ONNX Runtime inside libsherpa-onnx-jni.so: the other flavour ships its own
// libonnxruntime.so, which would collide with the one Java ONNX Runtime (Silero VAD, :core) brings. It is not on
// Maven Central, so the build fetches it from the pinned GitHub release into the build directory, checks the
// SHA-256 and drops the ABIs this app does not ship (the Phonon engine builds arm64-v8a and x86_64 only; the 32-bit
// and x86 libraries would add 48 MB and an x86 libonnxruntime.so that clashes with Java ONNX Runtime's). The binary
// is never committed. It is a file dependency, which a library module cannot publish: that is why the voice lives
// in this module and :assistant only sees the NeuralReplyVoice interface.
//
// The version is pinned to 1.13.8, the runtime the desktop sidecar speaks with (sherpa-onnx-go v1.13.8). It is not the
// newest release on purpose: sherpa-onnx v2.0.0 dropped the espeak-ng frontend, and the Piper voices are phoneme
// models - without the frontend they cannot turn text into token IDs at all ("phoneme_codepoints is empty, no
// lexicon, and no frontend available"). Since v2.0.0 a text-to-phoneme conversion needs an external phonemizer and a
// lexicon, which the shipped voices do not carry. 1.13.8 still phonemizes with espeak-ng from the voice archive's
// espeak-ng-data, so the phone speaks exactly like the desktop.
val sherpaOnnxVersion = "1.13.8"
val sherpaOnnxAarSha256 = "b22c3fc1b6a45666d28892bb2f7694beeb77a8362d7ebd77c1a5431ec9435471"
val sherpaOnnxDir = layout.buildDirectory.dir("sherpa-onnx")

fun sha256Of(file: File): String {
    val digest = MessageDigest.getInstance("SHA-256")
    file.inputStream().use { input ->
        val buffer = ByteArray(1 shl 16)
        while (true) {
            val n = input.read(buffer)
            if (n < 0) break
            digest.update(buffer, 0, n)
        }
    }
    return digest.digest().joinToString("") { "%02x".format(it) }
}

val fetchSherpaOnnxAar by tasks.registering {
    description = "Downloads sherpa-onnx $sherpaOnnxVersion (static-link-onnxruntime AAR) and checks its SHA-256"
    val target = sherpaOnnxDir.map { it.file("upstream/sherpa-onnx-static-link-onnxruntime-$sherpaOnnxVersion.aar").asFile }
    val url = "https://github.com/k2-fsa/sherpa-onnx/releases/download/v$sherpaOnnxVersion/" +
        "sherpa-onnx-static-link-onnxruntime-$sherpaOnnxVersion.aar"
    inputs.property("sha256", sherpaOnnxAarSha256)
    outputs.file(target)
    outputs.upToDateWhen { target.get().isFile && sha256Of(target.get()) == sherpaOnnxAarSha256 }
    doLast {
        val out = target.get()
        out.parentFile.mkdirs()
        val partial = File(out.parentFile, out.name + ".partial")
        val connection = URI(url).toURL().openConnection() as HttpURLConnection
        connection.instanceFollowRedirects = true
        connection.connectTimeout = 30_000
        connection.readTimeout = 60_000
        if (connection.responseCode != 200) {
            throw GradleException("sherpa-onnx download failed: HTTP ${connection.responseCode} for $url")
        }
        connection.inputStream.use { input -> partial.outputStream().use { input.copyTo(it) } }
        val got = sha256Of(partial)
        if (got != sherpaOnnxAarSha256) {
            partial.delete()
            throw GradleException("sherpa-onnx AAR failed verification: SHA-256 $got, expected $sherpaOnnxAarSha256")
        }
        if (out.exists()) out.delete()
        if (!partial.renameTo(out)) throw GradleException("could not install $out")
    }
}

val trimSherpaOnnxAar by tasks.registering(Zip::class) {
    description = "Repackages the sherpa-onnx AAR with only the ABIs the app ships"
    dependsOn(fetchSherpaOnnxAar)
    from(zipTree(sherpaOnnxDir.map { it.file("upstream/sherpa-onnx-static-link-onnxruntime-$sherpaOnnxVersion.aar") })) {
        exclude("jni/armeabi-v7a/**", "jni/x86/**")
    }
    destinationDirectory.set(sherpaOnnxDir.map { it.dir("trimmed") })
    archiveFileName.set("sherpa-onnx-static-link-onnxruntime-$sherpaOnnxVersion.aar")
    isPreserveFileTimestamps = false
    isReproducibleFileOrder = true
}

dependencies {
    implementation(project(":core"))
    implementation(project(":domain"))
    implementation(project(":assistant"))
    // Manifest merger lifts the voice IME service (`android.view.InputMethod`)
    // from :ime's library manifest into this APK — an IME only exists for the
    // system if it ships inside an installed application.
    implementation(project(":ime"))
    implementation(project(":net"))
    implementation(project(":coinstall"))
    implementation(project(":voice-ui-compose"))
    // On-device neural voices: the sherpa-onnx runtime (fetched above) and bzip2/tar for the voice archives.
    implementation(files(trimSherpaOnnxAar.flatMap { it.archiveFile }))
    implementation(libs.commons.compress)
    // The HeliBoard keyboard. GPL-3.0: linking it puts this APK under GPL-3.0
    // as a whole, so no proprietary Kombify code may enter here. Companion
    // integration stays IPC-only. See android/HELIBOARD.md.
    implementation(project(":heliboard"))

    // Compose
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.material3)
    implementation(libs.compose.ui.tooling.preview)
    debugImplementation(libs.compose.ui.tooling)
    implementation(libs.activity.compose)

    // Navigation
    implementation(libs.navigation.compose)
    implementation(libs.hilt.navigation.compose)

    // Lifecycle
    implementation(libs.lifecycle.runtime)
    implementation(libs.lifecycle.viewmodel)
    implementation(libs.lifecycle.viewmodel.compose)

    // DI
    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)

    // Logging
    implementation(libs.timber)

    // Testing
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.espresso.core)
}

// Kotlin 2.4 turned the kotlinOptions DSL from deprecated into an error.
kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
