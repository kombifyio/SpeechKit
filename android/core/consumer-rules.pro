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

# The Phonon engine (libspeechkit_phonon.so) binds its JNI functions by class and method name
# (Java_io_kombify_speechkit_stt_phonon_PhononNative_*), so neither may be renamed.
-keep class io.kombify.speechkit.stt.phonon.PhononNative { native <methods>; }
