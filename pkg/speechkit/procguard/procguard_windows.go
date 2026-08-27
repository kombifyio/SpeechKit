//go:build windows

package procguard

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
	jobErr    error
)

// ensureJob creates the process-wide Job Object on first use.
//
// The handle is intentionally never closed. JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// terminates every assigned process when the LAST handle to the job closes,
// and the only handle is this one — so it closes exactly when this process
// exits, by any route including an unhandled crash or an external kill. That
// is the whole point: no Go-side cleanup path has to run for the children to
// die with us.
func ensureJob() (windows.Handle, error) {
	jobOnce.Do(func() {
		handle, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = fmt.Errorf("create job object: %w", err)
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			handle,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			_ = windows.CloseHandle(handle)
			jobErr = fmt.Errorf("set kill-on-job-close limit: %w", err)
			return
		}
		jobHandle = handle
	})
	return jobHandle, jobErr
}

// Adopt assigns an already-started child to the kill-on-close job.
//
// Call it immediately after cmd.Start(). A child that spawns its own children
// in the window between Start and Adopt keeps those grandchildren outside the
// job; none of SpeechKit's sidecars do that, and closing the window would mean
// creating every child suspended.
//
// Assignment is a robustness measure, not a precondition for the child to
// work: callers should log a failure and carry on rather than tear down a
// process that is already running fine.
func Adopt(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("procguard: process has not been started")
	}
	job, err := ensureJob()
	if err != nil {
		return err
	}
	// PROCESS_SET_QUOTA and PROCESS_TERMINATE are exactly the rights
	// AssignProcessToJobObject requires.
	child, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open child process %d: %w", cmd.Process.Pid, err)
	}
	defer func() { _ = windows.CloseHandle(child) }()

	if err := windows.AssignProcessToJobObject(job, child); err != nil {
		return fmt.Errorf("assign process %d to job: %w", cmd.Process.Pid, err)
	}
	return nil
}
