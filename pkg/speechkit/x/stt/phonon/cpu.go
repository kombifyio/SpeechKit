package phonon

import (
	"runtime"

	"golang.org/x/sys/cpu"
)

// CPUSupported reports whether this processor runs the five-value engine
// (pkg/speechkit/x/stt/phonon/engine): every x86-64-v3 feature (AVX2, FMA,
// BMI1, BMI2, F16C, MOVBE and LZCNT, with the OS saving AVX state) on amd64,
// the dot-product extension on arm64. Hosts use it to show the Phonon models
// as unavailable on older processors instead of starting a sidecar that
// refuses to load them.
func CPUSupported() bool {
	switch runtime.GOARCH {
	case "amd64":
		return cpu.X86.HasAVX2 && cpu.X86.HasFMA && cpu.X86.HasBMI1 && cpu.X86.HasBMI2 && x86v3Extras()
	case "arm64":
		return cpu.ARM64.HasASIMDDP
	}
	return false
}
