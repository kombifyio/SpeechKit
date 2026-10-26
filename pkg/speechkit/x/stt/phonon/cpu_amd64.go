package phonon

// cpuid executes the CPUID instruction (cpuid_amd64.s).
func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

// x86v3Extras reports the x86-64-v3 features golang.org/x/sys/cpu does not
// expose: F16C and MOVBE (CPUID leaf 1, ECX bits 29 and 22) and LZCNT (leaf
// 0x80000001, ECX bit 5). The engine is compiled with -march=x86-64-v3, so
// the compiler may emit any of them.
func x86v3Extras() bool {
	maxLeaf, _, _, _ := cpuid(0, 0)
	if maxLeaf < 1 {
		return false
	}
	_, _, ecx1, _ := cpuid(1, 0)
	if ecx1&(1<<29) == 0 || ecx1&(1<<22) == 0 {
		return false
	}
	maxExt, _, _, _ := cpuid(0x80000000, 0)
	if maxExt < 0x80000001 {
		return false
	}
	_, _, ecxExt, _ := cpuid(0x80000001, 0)
	return ecxExt&(1<<5) != 0
}
