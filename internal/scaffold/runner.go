package scaffold

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
)

// runHooks executes PostInitHooks sequentially in workdir. The first
// failing hook short-circuits the chain. Output is appended to out as
// hooks run so callers see live progress.
func runHooks(workdir string, hooks []PostInitHook, out io.Writer) ([]HookResult, error) {
	if len(hooks) == 0 {
		return nil, nil
	}
	results := make([]HookResult, 0, len(hooks))
	for _, h := range hooks {
		fmt.Fprintf(out, "-> %s\n", describeHook(h))
		res, err := runOne(workdir, h)
		results = append(results, res)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

func describeHook(h PostInitHook) string {
	if strings.TrimSpace(h.Description) != "" {
		return fmt.Sprintf("%s (%s)", h.Description, h.Cmd)
	}
	return h.Cmd
}

func runOne(workdir string, h PostInitHook) (HookResult, error) {
	parts := strings.Fields(strings.TrimSpace(h.Cmd))
	if len(parts) == 0 {
		return HookResult{Cmd: h.Cmd, Description: h.Description}, fmt.Errorf("scaffold: empty hook cmd")
	}
	bin := parts[0]
	if runtime.GOOS == "windows" && bin == "npm" {
		// On Windows the npm shim is `npm.cmd`; resolve it once so the
		// hook works in cmd.exe / PowerShell child processes alike.
		bin = "npm.cmd"
	}
	cmd := exec.Command(bin, parts[1:]...) //nolint:gosec // template-author-controlled hook
	cmd.Dir = workdir
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	err := cmd.Run()
	res := HookResult{
		Description: h.Description,
		Cmd:         h.Cmd,
		Success:     err == nil,
		Output:      combined.String(),
	}
	if err != nil {
		return res, fmt.Errorf("scaffold hook %q failed: %w\n%s", h.Cmd, err, combined.String())
	}
	return res, nil
}
