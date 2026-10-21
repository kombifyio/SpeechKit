//go:build darwin

package procguard

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

func TestPrepareMovesTheChildIntoItsOwnGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	Prepare(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("Prepare did not ask for a new process group")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	if pgid != cmd.Process.Pid {
		t.Fatalf("child pgid = %d, want its own pid %d", pgid, cmd.Process.Pid)
	}
	self, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid(self): %v", err)
	}
	if pgid == self {
		t.Fatalf("child shares the test process's group %d", self)
	}
}

// Adopting a child that shares our group would arm Shutdown to signal the
// whole application — the desktop host, its windows and the user's dictation
// session — so it has to be refused rather than accepted quietly.
func TestAdoptRefusesAChildInOurOwnProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	err := Adopt(cmd)
	if err == nil {
		t.Fatal("Adopt accepted a child that was started without Prepare")
	}
	t.Logf("Adopt refused as expected: %v", err)
}

func TestAdoptRejectsProcessesThatAreNotRunning(t *testing.T) {
	if err := Adopt(nil); err == nil {
		t.Fatal("Adopt(nil) returned no error")
	}
	if err := Adopt(exec.Command("/bin/sh", "-c", "true")); err == nil {
		t.Fatal("Adopt of an unstarted command returned no error")
	}
}

func TestPrepareAdoptAndShutdownKillTheChild(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	Prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if err := Adopt(cmd); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	Shutdown()

	if err := cmd.Wait(); err == nil {
		t.Fatal("child exited cleanly, want a signal-terminated exit")
	}
	if syscall.Kill(cmd.Process.Pid, syscall.Signal(0)) == nil {
		t.Fatal("child is still running after Shutdown")
	}
}

// The sweep is driven against a fake process table and a fake signal sender:
// a real orphan cannot be manufactured inside a test, and a test that could
// send real signals is a test that could kill the runner.
func TestSweepSignalsOnlyTheProcessesInsideTheRoot(t *testing.T) {
	const root = "/Applications/SpeechKit.app/Contents/Helpers"
	table := []Process{
		{PID: 4001, ExecutablePath: root + "/whisper-server"},
		{PID: 4002, ExecutablePath: "/opt/homebrew/bin/whisper-server"},
	}

	var mu sync.Mutex
	signalled := map[int][]syscall.Signal{}
	restoreList, restoreSignal := listProcesses, signalProcess
	t.Cleanup(func() { listProcesses, signalProcess = restoreList, restoreSignal })
	listProcesses = func() ([]Process, error) { return table, nil }
	signalProcess = func(pid int, sig syscall.Signal) error {
		mu.Lock()
		defer mu.Unlock()
		signalled[pid] = append(signalled[pid], sig)
		if sig == syscall.Signal(0) {
			// Report the process as gone after the SIGTERM, so the sweep has
			// no reason to escalate to SIGKILL.
			return syscall.ESRCH
		}
		return nil
	}

	count, err := Sweep(root)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if count != 1 {
		t.Fatalf("Sweep terminated %d processes, want 1", count)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(signalled[4002]) != 0 {
		t.Fatalf("the user's own whisper-server was signalled with %v", signalled[4002])
	}
	if len(signalled[4001]) == 0 || signalled[4001][0] != syscall.SIGTERM {
		t.Fatalf("our stale helper got %v, want SIGTERM first", signalled[4001])
	}
}

func TestSweepRefusesARootItCannotTrust(t *testing.T) {
	for _, root := range []string{"", "   ", "Contents/Helpers"} {
		if _, err := Sweep(root); err == nil {
			t.Fatalf("Sweep(%q) returned no error", root)
		}
	}
}

// listOwnProcesses is the one part that needs a real kernel. It must find
// this test binary, with a path — the evidence the sweep is built on.
func TestListOwnProcessesReportsAbsoluteExecutablePaths(t *testing.T) {
	processes, err := listOwnProcesses()
	if err != nil {
		t.Fatalf("listOwnProcesses: %v", err)
	}
	if len(processes) == 0 {
		t.Fatal("listOwnProcesses returned nothing; the current user always has processes")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	path, err := executablePath(os.Getpid())
	if err != nil {
		t.Fatalf("executablePath(self): %v", err)
	}
	if path != self {
		t.Logf("executablePath(self) = %q, os.Executable = %q (symlinks and relative argv[0] differ legitimately)", path, self)
	}
	if path == "" {
		t.Fatal("executablePath(self) is empty")
	}
}

// A process that has exited has no argument vector, so the sweep skips it
// rather than treating an unreadable path as a match.
func TestExecutablePathFailsForAProcessThatHasExited(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for child: %v", err)
	}
	if path, err := executablePath(pid); err == nil {
		t.Fatalf("executablePath(%d) = %q for a reaped process, want an error", pid, path)
	}
}
