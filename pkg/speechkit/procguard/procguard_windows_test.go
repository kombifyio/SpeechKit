//go:build windows

package procguard

import (
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobObjectBasicProcessIDList mirrors JOBOBJECT_BASIC_PROCESS_ID_LIST, which
// golang.org/x/sys/windows does not declare. Only the two counters are fixed;
// the PID array that follows is variable length.
type jobObjectBasicProcessIDList struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
	firstProcessID            uintptr
}

const jobObjectBasicProcessIDListClass = 3

// assignedPIDs reads back the PIDs the job currently holds.
func assignedPIDs(t *testing.T, job windows.Handle) []uintptr {
	t.Helper()
	buf := make([]byte, 4096)
	var retlen uint32
	if err := windows.QueryInformationJobObject(
		job,
		jobObjectBasicProcessIDListClass,
		uintptr(unsafe.Pointer(&buf[0])),
		uint32(len(buf)),
		&retlen,
	); err != nil {
		t.Fatalf("QueryInformationJobObject: %v", err)
	}
	list := (*jobObjectBasicProcessIDList)(unsafe.Pointer(&buf[0]))
	if list.NumberOfProcessIdsInList == 0 {
		return nil
	}
	return unsafe.Slice(&list.firstProcessID, int(list.NumberOfProcessIdsInList))
}

func TestAdoptRejectsProcessesThatAreNotRunning(t *testing.T) {
	if err := Adopt(nil); err == nil {
		t.Fatal("Adopt(nil) returned no error")
	}
	if err := Adopt(exec.Command("cmd.exe", "/c", "rem")); err == nil {
		t.Fatal("Adopt of an unstarted command returned no error")
	}
}

// A started child must end up inside the job, because that membership is the
// whole mechanism: when this process dies the last job handle closes and
// Windows terminates everything in it. Without membership the child survives
// the parent and becomes one of the ~1 GB orphans this package exists to
// prevent.
func TestAdoptPutsTheChildInTheKillOnCloseJob(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping", "-n", "30", "127.0.0.1")
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

	job, err := ensureJob()
	if err != nil {
		t.Fatalf("ensureJob: %v", err)
	}
	want := uintptr(cmd.Process.Pid)
	for _, pid := range assignedPIDs(t, job) {
		if pid == want {
			return
		}
	}
	t.Fatalf("child pid %d is not assigned to the job", cmd.Process.Pid)
}
