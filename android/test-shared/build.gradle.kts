import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "io.kombify.speechkit.test"
    compileSdk = 36

    defaultConfig {
        minSdk = 26
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
    implementation(libs.coroutines.core)
    implementation(libs.coroutines.test)
    implementation(libs.mockk)
    implementation(libs.turbine)
    implementation(libs.junit.api)
}

// Kotlin 2.4 turned the kotlinOptions DSL from deprecated into an error.
kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}
