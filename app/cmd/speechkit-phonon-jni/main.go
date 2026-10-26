//go:build android && cgo && (amd64 || arm64)

// speechkit-phonon-jni is the Android native library (libspeechkit_phonon.so)
// behind io.kombify.speechkit.stt.phonon.PhononNative: SpeechKit's five-value
// engine for Phonon-2.5 (pkg/speechkit/x/stt/phonon/engine) with its Go model
// loader, called from Kotlin through JNI. The Android core module builds it
// with the NDK:
//
//	GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC=<ndk>/aarch64-linux-android26-clang \
//	    go build -buildmode=c-shared -o libspeechkit_phonon.so ./app/cmd/speechkit-phonon-jni
//
// Every function runs on the calling Java thread. A model is a handle (a
// positive number) that stays valid until close; failures throw
// java.io.IOException or java.lang.IllegalStateException with the reason.
package main

/*
#include <jni.h>
#include <stdlib.h>

static const char *sk_utf(JNIEnv *env, jstring s) { return (*env)->GetStringUTFChars(env, s, NULL); }
static void sk_release_utf(JNIEnv *env, jstring s, const char *c) { (*env)->ReleaseStringUTFChars(env, s, c); }
static jsize sk_float_len(JNIEnv *env, jfloatArray a) { return (*env)->GetArrayLength(env, a); }
static void sk_floats(JNIEnv *env, jfloatArray a, jsize n, float *dst) { (*env)->GetFloatArrayRegion(env, a, 0, n, dst); }
static jbyteArray sk_bytes(JNIEnv *env, const char *p, jsize n) {
	jbyteArray out = (*env)->NewByteArray(env, n);
	if (out != NULL && n > 0) (*env)->SetByteArrayRegion(env, out, 0, n, (const jbyte *)p);
	return out;
}
static jsize sk_array_len(JNIEnv *env, jobjectArray a) { return (*env)->GetArrayLength(env, a); }
static jstring sk_string_at(JNIEnv *env, jobjectArray a, jsize i) { return (jstring)(*env)->GetObjectArrayElement(env, a, i); }
static void sk_delete_local(JNIEnv *env, jobject o) { (*env)->DeleteLocalRef(env, o); }
static void sk_throw(JNIEnv *env, const char *cls, const char *msg) {
	jclass c = (*env)->FindClass(env, cls);
	if (c != NULL) (*env)->ThrowNew(env, c, msg);
}
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon/engine"
)

var (
	mu     sync.Mutex
	models = map[int64]*engine.Recognizer{}
	nextID int64
)

func throw(env *C.JNIEnv, class, msg string) {
	cls, cmsg := C.CString(class), C.CString(msg)
	defer C.free(unsafe.Pointer(cls))
	defer C.free(unsafe.Pointer(cmsg))
	C.sk_throw(env, cls, cmsg)
}

// goString copies a Java string; ok is false when an exception is pending.
func goString(env *C.JNIEnv, s C.jstring) (string, bool) {
	c := C.sk_utf(env, s)
	if c == nil {
		return "", false
	}
	defer C.sk_release_utf(env, s, c)
	return C.GoString(c), true
}

func lookup(handle C.jlong) *engine.Recognizer {
	mu.Lock()
	defer mu.Unlock()
	return models[int64(handle)]
}

//export Java_io_kombify_speechkit_stt_phonon_PhononNative_cpuSupported
func Java_io_kombify_speechkit_stt_phonon_PhononNative_cpuSupported(env *C.JNIEnv, cls C.jclass) C.jboolean {
	if phonon.CPUSupported() {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}

//export Java_io_kombify_speechkit_stt_phonon_PhononNative_open
func Java_io_kombify_speechkit_stt_phonon_PhononNative_open(env *C.JNIEnv, cls C.jclass, dir C.jstring, threads C.jint) C.jlong {
	modelDir, ok := goString(env, dir)
	if !ok {
		return 0 // OutOfMemoryError is pending
	}
	r, err := engine.NewRecognizer(phonon.Config{ModelDir: modelDir, Threads: int(threads)})
	if err != nil {
		throw(env, "java/io/IOException", err.Error())
		return 0
	}
	mu.Lock()
	defer mu.Unlock()
	nextID++
	models[nextID] = r
	return C.jlong(nextID)
}

//export Java_io_kombify_speechkit_stt_phonon_PhononNative_transcribe
func Java_io_kombify_speechkit_stt_phonon_PhononNative_transcribe(env *C.JNIEnv, cls C.jclass, handle C.jlong, samples C.jfloatArray) C.jbyteArray {
	r := lookup(handle)
	if r == nil {
		throw(env, "java/lang/IllegalStateException", "phonon: the model is closed")
		return 0
	}
	audio := make([]float32, int(C.sk_float_len(env, samples)))
	if len(audio) > 0 {
		C.sk_floats(env, samples, C.jsize(len(audio)), (*C.float)(unsafe.Pointer(&audio[0])))
	}
	result, err := r.Transcribe(audio)
	if err != nil {
		throw(env, "java/lang/IllegalStateException", err.Error())
		return 0
	}
	text := []byte(result.Text)
	if len(text) == 0 {
		return C.sk_bytes(env, nil, 0)
	}
	return C.sk_bytes(env, (*C.char)(unsafe.Pointer(&text[0])), C.jsize(len(text)))
}

//export Java_io_kombify_speechkit_stt_phonon_PhononNative_close
func Java_io_kombify_speechkit_stt_phonon_PhononNative_close(env *C.JNIEnv, cls C.jclass, handle C.jlong) {
	mu.Lock()
	r := models[int64(handle)]
	delete(models, int64(handle))
	mu.Unlock()
	if r != nil {
		r.Close()
	}
}

// unpack extracts the named files of a model archive (Fermion's
// zstd-compressed tar) into dir; see unpack.go.
//
//export Java_io_kombify_speechkit_stt_phonon_PhononNative_unpack
func Java_io_kombify_speechkit_stt_phonon_PhononNative_unpack(env *C.JNIEnv, cls C.jclass, archive, dir C.jstring, names C.jobjectArray) {
	archivePath, ok := goString(env, archive)
	if !ok {
		return
	}
	dirPath, ok := goString(env, dir)
	if !ok {
		return
	}
	list := make([]string, int(C.sk_array_len(env, names)))
	for i := range list {
		js := C.sk_string_at(env, names, C.jsize(i))
		name, ok := goString(env, js)
		C.sk_delete_local(env, C.jobject(js))
		if !ok {
			return
		}
		list[i] = name
	}
	if err := unpack(archivePath, dirPath, list); err != nil {
		throw(env, "java/io/IOException", err.Error())
	}
}

func main() {}
