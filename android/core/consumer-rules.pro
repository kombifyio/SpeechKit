# Consumer rules for speechkit-sdk AAR
-keep class io.kombify.speechkit.engine.** { *; }
-keep class io.kombify.speechkit.stt.SttProvider { *; }
-keep class io.kombify.speechkit.stt.Result { *; }
-keep class io.kombify.speechkit.store.Store { *; }
-keep class io.kombify.speechkit.audio.AudioSession { *; }

# ONNX Runtime's native code constructs its Java types (tensors, node and model info, exceptions) through JNI,
# which R8 cannot see; its AAR keeps only its telemetry classes. Without this a host's minified release build
# fails the first on-device model it loads.
-keep class ai.onnxruntime.** { *; }
