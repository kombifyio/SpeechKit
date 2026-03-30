# kombify SpeechKit ProGuard Rules

# Keep ONNX Runtime classes
-keep class ai.onnxruntime.** { *; }

# Keep Moshi adapters
-keep class io.kombify.speechkit.stt.TranscriptionResponse { *; }
-keep class io.kombify.speechkit.ai.Chat* { *; }

# Keep Room entities
-keep class io.kombify.speechkit.store.*Entity { *; }
