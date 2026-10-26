# Vendored phonon-2-c

The C sources in this directory are the library half of
[phonon-2-c](https://github.com/eschmidbauer/phonon-2-c) (MIT, see
`LICENSE.phonon-2-c` and `NOTICE.phonon-2-c`), a pure-C inference engine for
Fermion Research's five-value Parakeet TDT models.

- Upstream commit: `c4b860f6` (2026-10-09), files `csrc/*.c` and `csrc/*.h`
  without the CLI (`main.c`) and the GEMM benchmark.
- Imported: 2026-10-26.

## SpeechKit changes

- Every `.c` file starts with `//go:build cgo && (amd64 || arm64)`, so the Go
  package still builds without cgo.
- `p2_compat.h` (new) wraps aligned allocation, the online CPU count and the
  read-only file mapping, so the sources build with MinGW on Windows:
  `blas.c` allocates and frees its 64-byte aligned buffers through
  `p2_aligned_alloc` and `p2_aligned_free` (Windows needs `_aligned_free`),
  `threadpool.c` counts CPUs through `p2_online_cpus`, and `weights.c` maps
  `weights.bin` through `p2_map_readonly` and `p2_unmap`. On Android below API
  level 28, where bionic declares no `aligned_alloc`, `p2_aligned_alloc` uses
  `posix_memalign` (the Android library `app/cmd/speechkit-phonon-jni` targets
  API 26).
- The build flags come from the cgo preamble in `recognizer_cgo.go` instead
  of `-march=native`: x86-64-v3 on amd64, the dot-product extension on
  arm64. `NewRecognizer` checks the processor first (`phonon.CPUSupported`,
  which reads CPUID for the x86-64-v3 features `golang.org/x/sys/cpu` lacks).
- `blas_i8_avx2.h` (new) ports the int8 dot-product GEMM of `blas.c` (the
  SDOT block: activation quantisation, panel packing, the 4 × 16 kernel and
  its epilogue) to AVX2 with the same layouts and exact integer sums, so x86
  runs the int8 modes too instead of only the fp32 expansion. `blas.c`
  defines `P2_I8_AVX2` and `P2_INT8` after `P2_DOTPROD`, includes the file
  in place of the SDOT block, and tests `P2_INT8` where it tested
  `P2_DOTPROD` for the mode default and the two GEMM entry points.

SpeechKit loads the weights with `p2_engine_open_mem` from the image
`pkg/speechkit/x/stt/phonon` builds from Fermion's `model.fermion` in Go, so
the converter script and the `weights.bin` file format on disk are not used.

To update: copy the same files from a newer upstream commit, reapply the
changes above, and record the commit here.
