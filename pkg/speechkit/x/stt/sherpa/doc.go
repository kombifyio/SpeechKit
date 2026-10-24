// Package sherpa runs NeMo transducer speech-to-text models on the CPU through
// sherpa-onnx: NVIDIA Parakeet TDT 0.6B v3 and its post-trained derivatives
// such as Moondream's Parakeet Ultra, exported to sherpa-onnx's int8
// nemo_transducer layout (encoder, decoder, joiner and tokens.txt in one
// directory).
//
// The package is pure Go. Its subpackage engine owns the native model and
// turns 16 kHz mono samples into text, cutting recordings longer than about
// 30 seconds at pauses first ([SplitAtPauses]; the int8 export loses accuracy
// on long single passes). [NewHandler] serves any [Transcriber] over the same
// HTTP contract as whisper.cpp's whisper-server (GET /health, the
// public-directory identity file and a multipart POST on the inference path),
// so the cmd speechkit-stt-sherpa sidecar slots into every place SpeechKit
// already drives whisper-server: the built-in local provider, the self-hosted
// [vps] route and the Box media runtime.
//
// Hosts that only talk to the sidecar (the local provider) import this
// package and never link sherpa-onnx; only the sidecar imports engine.
//
// Stability: Experimental — may change in any release.
package sherpa
