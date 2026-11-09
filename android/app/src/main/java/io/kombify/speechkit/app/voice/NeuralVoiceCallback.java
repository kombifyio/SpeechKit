package io.kombify.speechkit.app.voice;

import kotlin.jvm.functions.Function1;

/**
 * The callback sherpa-onnx calls while it synthesizes a voice.
 *
 * <p>sherpa's JNI layer does not call the Kotlin function type directly. It looks the method up by name and
 * signature with {@code GetMethodID(cls, "invoke", "([F)Ljava/lang/Integer;")} and then calls it, so the class
 * behind the callback has to carry exactly that method: {@code invoke(float[])} returning a boxed
 * {@link java.lang.Integer}.
 *
 * <p>Kotlin cannot express that method. A Kotlin lambda is desugared by D8 into a class with the generic bridge
 * {@code invoke(Object): Object} only, and a Kotlin function type {@code (FloatArray) -> Int} maps {@code Int} to
 * the primitive {@code int}, which the JNI lookup does not find. The result is a {@code NoSuchMethodError} in the
 * middle of the first synthesis and a crashed process.
 *
 * <p>A Java class can express it: {@code Integer} is a reference type there, so the method keeps its
 * {@code ([F)Ljava/lang/Integer;} descriptor through javac, D8 and ProGuard.
 *
 * <p>The actual work stays in Kotlin: [Handler] carries the callback body, this class only keeps the signature the
 * native code looks for.
 */
public final class NeuralVoiceCallback implements Function1<float[], Integer> {

    /** The part of the callback that is written in Kotlin. */
    public interface Handler {
        /**
         * Handles one block of samples.
         *
         * @param samples the synthesized audio samples, in the range of -1 to 1
         * @return 1 to continue the synthesis, 0 to stop it
         */
        int onSamples(float[] samples);
    }

    private final Handler handler;

    public NeuralVoiceCallback(Handler handler) {
        this.handler = handler;
    }

    /**
     * Called by sherpa's JNI layer. The descriptor {@code ([F)Ljava/lang/Integer;} is the one the native code
     * looks up by name, so it must not change.
     */
    @Override
    public Integer invoke(float[] samples) {
        return Integer.valueOf(handler.onSamples(samples));
    }
}
