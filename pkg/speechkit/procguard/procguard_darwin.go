//go:build darwin

package procguard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// macOS has no Job Object and no PR_SET_PDEATHSIG. The closest thing to a
// parent-death guarantee is the process group: a child started in its own
// group can be signalled as a unit, grandchildren included, without ever
// naming a PID that might have been recycled onto some other process in the
// meantime.
//
// Everything below is built on that one primitive plus a startup sweep, and
// each piece is deliberately narrow about what it is allowed to kill:
//
//   - Prepare/Adopt only ever record a group this process created.
//   - Shutdown refuses to signal this process's own group, so a missing
//     Prepare degrades to "the child is not adopted" instead of "the
//     application kills itself".
//   - Sweep only kills processes whose executable image lives inside the
//     directory the host names, which for the desktop client is our own
//     application bundle.

const (
	// terminateGrace is how long a process gets to exit after SIGTERM before
	// it is killed.
	//
	// One second is generous for these children: whisper-server and
	// llama-server hold nothing but a model and a log, so the grace is about
	// letting a clean exit finish, not about saving data. It is also the
	// upper bound on how long quitting the app can take because of this
	// package, which is why it is not longer — a child that has exited but
	// has not yet been reaped by its owner still answers signal 0, so a
	// generous window would be spent in full on a corpse.
	terminateGrace = 1 * time.Second
	// gonePollInterval is how often a signalled process is re-checked.
	gonePollInterval = 50 * time.Millisecond
)

var (
	adoptedMu     sync.Mutex
	adoptedGroups = map[int]struct{}{}
	signalsOnce   sync.Once

	// listProcesses is a variable so a test can hand the sweep a fake
	// process table; the real one needs processes that only a Mac has.
	listProcesses = listOwnProcesses
	// signalProcess and signalGroup are variables for the same reason.
	signalProcess = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
	signalGroup   = func(pgid int, sig syscall.Signal) error { return syscall.Kill(-pgid, sig) }
)

// Prepare puts the child in its own process group. Call it before cmd.Start;
// after the child has exec'd, the parent can no longer change its group
// (setpgid answers EACCES), so there is no way to repair a missed call.
//
// It cannot fail — it only fills in a field — so it returns nothing. The
// honest report about whether the child ended up tied to this process is
// [Adopt]'s error.
func Prepare(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Setpgid with a zero Pgid makes the child the leader of a new group
	// whose id is its own PID. A side effect worth knowing: the child no
	// longer receives the terminal's Ctrl-C, which is exactly why Adopt
	// installs a SIGINT handler of its own.
	cmd.SysProcAttr.Setpgid = true
}

// Adopt records the child's process group so [Shutdown] can terminate it, and
// installs the signal handler that makes a terminal kill of the host run that
// shutdown.
//
// Call it immediately after cmd.Start(). A child that was started without
// [Prepare] shares this process's group; adopting it would arm Shutdown to
// signal the whole application, so that case is refused with an error rather
// than accepted silently.
func Adopt(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("procguard: process has not been started")
	}
	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("procguard: read process group of %d: %w", pid, err)
	}
	self, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		return fmt.Errorf("procguard: read own process group: %w", err)
	}
	if pgid == self {
		return fmt.Errorf("procguard: child %d is in this process's own group %d; Prepare must run before Start", pid, pgid)
	}
	adoptedMu.Lock()
	adoptedGroups[pgid] = struct{}{}
	adoptedMu.Unlock()
	installSignalHandler()
	return nil
}

// Shutdown terminates every adopted process group: SIGTERM, a grace period,
// then SIGKILL for whatever is still there.
//
// The host registers it at the very bottom of its cleanup stack so it runs
// last, after each subsystem has had its own chance to close its child
// politely. It is safe to call more than once; the second call has nothing
// left to signal.
func Shutdown() {
	adoptedMu.Lock()
	groups := make([]int, 0, len(adoptedGroups))
	for pgid := range adoptedGroups {
		groups = append(groups, pgid)
	}
	adoptedGroups = map[int]struct{}{}
	adoptedMu.Unlock()
	if len(groups) == 0 {
		return
	}

	self, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		// Without knowing our own group we cannot prove a signal will not
		// come back at us, so we send none at all.
		return
	}
	live := groups[:0]
	for _, pgid := range groups {
		if pgid <= 1 || pgid == self {
			continue
		}
		if err := signalGroup(pgid, syscall.SIGTERM); err == nil {
			live = append(live, pgid)
		}
	}
	waitUntilGone(terminateGrace, func() bool {
		for _, pgid := range live {
			if signalGroup(pgid, syscall.Signal(0)) == nil {
				return false
			}
		}
		return true
	})
	for _, pgid := range live {
		if signalGroup(pgid, syscall.Signal(0)) == nil {
			_ = signalGroup(pgid, syscall.SIGKILL)
		}
	}
}

// Sweep terminates processes left behind by an earlier run of this
// application: anything still running whose executable image lives inside
// root. It returns how many processes it signalled.
//
// The host calls it at startup, after the single-instance lock is held. That
// ordering is the argument that a match is an orphan and not a sibling: with
// the lock held there is no second instance, so nothing running out of our
// own bundle can belong to a live application.
//
// root must be an absolute directory that only this application's own files
// live in — the bundle's Contents/Helpers. Passing a directory that holds
// anything else would hand this function permission to kill it.
func Sweep(root string) (int, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return 0, errors.New("procguard: sweep root is empty")
	}
	if !filepath.IsAbs(root) {
		return 0, fmt.Errorf("procguard: sweep root %q is not absolute", root)
	}
	processes, err := listProcesses()
	if err != nil {
		return 0, fmt.Errorf("procguard: list processes: %w", err)
	}
	stale := staleUnder(processes, root, os.Getpid())
	if len(stale) == 0 {
		return 0, nil
	}

	// Signalled by PID rather than by group: these processes were reparented
	// to launchd when their host died, and a group id that old may since have
	// been handed to something else. The PID is proven ours by its executable
	// path; its group is not.
	live := make([]Process, 0, len(stale))
	for _, process := range stale {
		if err := signalProcess(process.PID, syscall.SIGTERM); err == nil {
			live = append(live, process)
		}
	}
	waitUntilGone(terminateGrace, func() bool {
		for _, process := range live {
			if signalProcess(process.PID, syscall.Signal(0)) == nil {
				return false
			}
		}
		return true
	})
	for _, process := range live {
		if signalProcess(process.PID, syscall.Signal(0)) == nil {
			_ = signalProcess(process.PID, syscall.SIGKILL)
		}
	}
	return len(live), nil
}

// waitUntilGone polls done until it reports true or the budget runs out.
func waitUntilGone(budget time.Duration, done func() bool) {
	deadline := time.Now().Add(budget)
	for {
		if done() {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(gonePollInterval)
	}
}

// installSignalHandler makes a terminal kill of the host clean up after
// itself.
//
// Prepare moved the children out of this process's group, so a Ctrl-C or a
// `kill` of the host no longer reaches them through the terminal. This
// handler restores that: it runs Shutdown, then puts the signal back to its
// default disposition and re-raises it, so the process still dies exactly
// the way the sender asked it to.
func installSignalHandler() {
	signalsOnce.Do(func() {
		received := make(chan os.Signal, 1)
		signal.Notify(received, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			raw, ok := <-received
			if !ok {
				return
			}
			Shutdown()
			signal.Stop(received)
			sig, ok := raw.(syscall.Signal)
			if !ok {
				return
			}
			signal.Reset(sig)
			_ = syscall.Kill(os.Getpid(), sig)
		}()
	})
}

// listOwnProcesses returns the running processes of the current user, with
// the absolute path of each executable image.
//
// Restricting the listing to our own uid is the first half of "only ever kill
// what is ours" — a process of another user could not be signalled anyway,
// and asking the kernel for its argument vector would only fail. It also
// keeps the sweep cheap: on a normal desktop this is tens of processes, not
// hundreds.
func listOwnProcesses() ([]Process, error) {
	uid := os.Getuid()
	infos, err := unix.SysctlKinfoProcSlice("kern.proc.uid", uid)
	if err != nil {
		return nil, fmt.Errorf("kern.proc.uid: %w", err)
	}
	processes := make([]Process, 0, len(infos))
	for i := range infos {
		pid := int(infos[i].Proc.P_pid)
		if pid <= 1 {
			continue
		}
		path, err := executablePath(pid)
		if err != nil || !filepath.IsAbs(path) {
			// A process that exited between the listing and the read, or one
			// whose arguments the kernel will not hand over, simply is not a
			// candidate. It is never a reason to fail the whole sweep.
			continue
		}
		processes = append(processes, Process{PID: pid, ExecutablePath: path})
	}
	return processes, nil
}

// executablePath reads the running image path of a process out of
// KERN_PROCARGS2.
//
// The buffer the kernel returns starts with a 32-bit argc, and the executable
// path follows it as a NUL-terminated string before the padding and the
// argument vector. This is the only way to get a full path without cgo:
// kinfo_proc carries just a 16-character command name, which is exactly the
// kind of evidence the sweep must not act on.
func executablePath(pid int) (string, error) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	const argcSize = 4
	if len(buf) <= argcSize {
		return "", errors.New("kern.procargs2: short buffer")
	}
	rest := buf[argcSize:]
	end := bytes.IndexByte(rest, 0)
	if end <= 0 {
		return "", errors.New("kern.procargs2: no executable path")
	}
	return string(rest[:end]), nil
}
