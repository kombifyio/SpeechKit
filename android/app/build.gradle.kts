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
        versionCode = 6822
        versionName = "0.68.22"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
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

    testOptions {
        // JUnit 5 (Jupiter) tests silently do not run without this.
        unitTests.all { it.useJUnitPlatform() }
    }
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
    testImplementation(libs.junit.api)
    testRuntimeOnly(libs.junit.engine)
    testImplementation(libs.coroutines.test)
    testImplementation(libs.mockk)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.espresso.core)
}

// Kotlin 2.4 turned the kotlinOptions DSL from deprecated into an error.
kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
