package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// runHooks executes PostInitHooks sequentially in workdir. The first
// failing hook short-circuits the chain. Output is appended to out as
// hooks run so callers see live progress.
func runHooks(ctx context.Context, workdir string, hooks []PostInitHook, out io.Writer) ([]HookResult, error) {
	if len(hooks) == 0 {
		return nil, nil
	}
	results := make([]HookResult, 0, len(hooks))
	for _, h := range hooks {
		if _, err := fmt.Fprintf(out, "-> %s\n", describeHook(h)); err != nil {
			return results, err
		}
		res, err := runOne(ctx, workdir, h)
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

func runOne(ctx context.Context, workdir string, h PostInitHook) (HookResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	cmd, err := hookCommand(ctx, workdir, h)
	if err != nil {
		return HookResult{Cmd: h.Cmd, Description: h.Description}, err
	}
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	err = cmd.Run()
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

func hookCommand(ctx context.Context, workdir string, h PostInitHook) (*exec.Cmd, error) {
	bin, args, err := parseHookCommand(h.Cmd)
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	switch {
	case bin == "npm" && len(args) == 1 && args[0] == "install":
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "npm.cmd", "install")
		} else {
			cmd = exec.CommandContext(ctx, "npm", "install")
		}
	case bin == "npm" && len(args) == 1 && args[0] == "ci":
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "npm.cmd", "ci")
		} else {
			cmd = exec.CommandContext(ctx, "npm", "ci")
		}
	default:
		return nil, fmt.Errorf("scaffold: hook command %q is not allowlisted", h.Cmd)
	}
	cmd.Dir = workdir
	return cmd, nil
}

func parseHookCommand(raw string) (string, []string, error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("scaffold: empty hook cmd")
	}
	for _, part := range parts {
		if strings.ContainsAny(part, "&;|<>`$()") {
			return "", nil, fmt.Errorf("scaffold: hook command %q contains shell syntax", raw)
		}
	}
	if parts[0] != "npm" {
		return "", nil, fmt.Errorf("scaffold: hook binary %q is not allowlisted", parts[0])
	}
	args := parts[1:]
	if len(args) != 1 || (args[0] != "install" && args[0] != "ci") {
		return "", nil, fmt.Errorf("scaffold: hook command %q is not allowlisted", raw)
	}
	return parts[0], args, nil
}
