import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
}

android {
    namespace = "io.kombify.speechkit"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.kombify.speechkit"
        minSdk = 26
        targetSdk = 36
        versionCode = 5800
        versionName = "0.58.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
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
