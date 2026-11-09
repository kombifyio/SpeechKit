# kombify SpeechKit ProGuard Rules

# Keep ONNX Runtime classes
-keep class ai.onnxruntime.** { *; }

# Keep Moshi adapters
-keep class io.kombify.speechkit.stt.TranscriptionResponse { *; }
-keep class io.kombify.speechkit.ai.Chat* { *; }

# Keep Room entities
-keep class io.kombify.speechkit.store.*Entity { *; }

# sherpa-onnx: its JNI library reads the fields of these Kotlin classes by name.
-keep class com.k2fsa.sherpa.onnx.** { *; }

# The callback of the neural voices: sherpa's JNI looks the method up by the name and
# signature "invoke([F)Ljava/lang/Integer;". Without this the method can be renamed or
# removed and the synthesis fails with a NoSuchMethodError.
-keep class io.kombify.speechkit.app.voice.NeuralVoiceCallback { *; }
-keep interface io.kombify.speechkit.app.voice.NeuralVoiceCallback$Handler { *; }

# commons-compress names optional codecs (xz, zstd, brotli, ...) this app does not ship; only bzip2 and tar are used.
-dontwarn org.apache.commons.compress.**
-dontwarn org.tukaani.xz.**
-dontwarn com.github.luben.zstd.**
-dontwarn org.brotli.dec.**
-dontwarn org.objectweb.asm.**
-dontwarn com.github.benmanes.caffeine.**
-dontwarn org.osgi.annotation.**
