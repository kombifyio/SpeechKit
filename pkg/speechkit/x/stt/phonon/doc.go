// Package phonon reads Fermion Research's Phonon-2.5 speech model and builds
// the weight image the vendored phonon-2-c engine loads.
//
// Fermion ships Phonon-2.5, a 25-language student of NVIDIA Parakeet TDT 0.6B
// v3 (and an English-only variant), as an unpacked archive: model.fermion,
// config.json and packed_manifest.json. model.fermion is a
// "fermion-five-value-parakeet-v1" container: five-value linears stored as
// packed trits, int6 and int8 tables, and fp16 tensors. [ReadContainer] reads
// it, keeping the raw quantized representations. [BuildImage] and [Load]
// convert it, at model load time, to the flat weights, JSON manifest and token
// list that phonon-2-c's p2_engine_open_mem takes, so a device stores only
// Fermion's original files. The output is byte-identical to the one
// eschmidbauer's Python converter writes.
//
// The container reader follows Fermion Research's fermion_container.py
// (Apache-2.0) and the image layout follows eschmidbauer/phonon-2-c's
// convert_weights.py (MIT). The model weights are licensed CC-BY-4.0.
//
// The package is pure Go and has no native dependency.
//
// Stability: Experimental — may change in any release.
package phonon
