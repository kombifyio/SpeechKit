package local

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"unicode/utf8"
)

// whisperModelArgument resolves the --model argument and working directory for
// the whisper-server child process.
//
// whisper.cpp receives argv through the C runtime in the ANSI code page but
// opens the model through a UTF-8 aware fopen wrapper. Any non-ASCII byte in
// the path — an umlaut in the Windows user name under %LOCALAPPDATA% is enough
// — therefore makes the child fail fast with STATUS_STACK_BUFFER_OVERRUN
// (0xc0000409) before the HTTP server is up, and local STT silently drops to
// cloud fallback. Passing an ASCII-only relative file name and running the
// child inside the model directory side-steps the argv encoding entirely; the
// working directory itself is handed to CreateProcessW as UTF-16 and is safe.
//
// When the file name itself is non-ASCII the platform short (8.3) name is used
// where the file system provides one. The returned dir is empty when the
// caller should not change the working directory.
func whisperModelArgument(modelPath string) (arg, dir string) {
	if isASCII(modelPath) {
		return modelPath, ""
	}
	base := filepath.Base(modelPath)
	if isASCII(base) {
		return base, filepath.Dir(modelPath)
	}
	if short := asciiShortPath(modelPath); short != "" {
		return short, ""
	}
	return modelPath, ""
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// describeProcessExit turns a whisper-server exit into an operator-readable
// cause. Windows reports crashes as NTSTATUS exit codes; the numeric value in
// "exit status 0xc0000409" tells nobody what to do next.
func describeProcessExit(waitErr error) string {
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return ""
	}
	return describeExitCode(exitErr.ExitCode())
}

func describeExitCode(code int) string {
	switch uint32(code) {
	case 0xc0000409:
		return "exit code 0xc0000409 STATUS_STACK_BUFFER_OVERRUN: whisper-server aborted while loading the model; a non-ASCII model path or a truncated/corrupt model file is the usual cause"
	case 0xc0000005:
		return "exit code 0xc0000005 STATUS_ACCESS_VIOLATION: whisper-server crashed; the model file may be corrupt or built for another whisper.cpp version"
	case 0xc000001d:
		return "exit code 0xc000001d STATUS_ILLEGAL_INSTRUCTION: this CPU lacks an instruction set (AVX2/AVX-512) the bundled whisper-server was compiled for"
	case 0xc000007b:
		return "exit code 0xc000007b STATUS_INVALID_IMAGE_FORMAT: a bundled DLL is damaged or has the wrong architecture"
	case 0xc0000135:
		return "exit code 0xc0000135 STATUS_DLL_NOT_FOUND: a bundled ggml/whisper runtime DLL is missing next to whisper-server"
	}
	if code < 0 || code > 0xffff {
		return fmt.Sprintf("exit code 0x%08x", uint32(code))
	}
	return fmt.Sprintf("exit code %d", code)
}
