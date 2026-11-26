import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    `maven-publish`
    alias(libs.plugins.android.library)
    alias(libs.plugins.ksp)
}

android {
    namespace = "io.kombify.speechkit.hybrid"
    compileSdk = 37
    defaultConfig { minSdk = 26 }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    publishing {
        singleVariant("release") { withSourcesJar() }
    }
}

publishing {
    publications {
        register<MavenPublication>("release") {
            groupId = "io.kombify.aiplatform.speechkit"
            artifactId = "hybrid"
            version = providers.fileContents(
                rootProject.layout.projectDirectory.file("../.kombify/VERSION"),
            ).asText.map { it.trim() }.getOrElse("0.0.0")
            afterEvaluate { from(components["release"]) }
            pom {
                name.set("SpeechKit Hybrid (Android)")
                description.set("Optional Firebase hybrid inference and local ML Kit speech and agent adapters.")
                licenses {
                    license {
                        name.set("Apache-2.0")
                        url.set("https://www.apache.org/licenses/LICENSE-2.0")
                    }
                }
                withXml {
                    val xml = asNode() as groovy.util.Node
                    val management = xml.appendNode("distributionManagement")
                    val repository = management.appendNode("repository")
                    repository.appendNode("id", "github")
                    repository.appendNode("name", "GitHub Packages")
                    repository.appendNode("url", "https://maven.pkg.github.com/kombifyio/SpeechKit")
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
    api(project(":core"))
    implementation(project(":domain"))
    implementation(project(":net"))
    implementation(libs.coroutines.core)
    implementation(platform(libs.okhttp.bom))
    implementation(libs.firebase.ai)
    implementation(libs.firebase.ai.ondevice)
    implementation(libs.mlkit.genai.agent)
    implementation(libs.mlkit.genai.speech)
    // NoteAppFunctions publicly extends AppFunctionService.
    api(libs.androidx.appfunctions)
    ksp(libs.androidx.appfunctions.compiler)
}

kotlin { compilerOptions { jvmTarget.set(JvmTarget.JVM_17) } }
