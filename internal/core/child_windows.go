//go:build windows

package core

import (
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A single kill-on-close job object holds every kernel this process starts.
// When zashhomo exits for any reason (clean exit, panic, or a forced kill) the
// OS closes the job handle and terminates the kernel with it — no orphans.
var (
	jobOnce sync.Once
	jobH    windows.Handle
	jobErr  error
)

func ensureJob() (windows.Handle, error) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
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
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			windows.CloseHandle(h)
			jobErr = fmt.Errorf("set job object info: %w", err)
			return
		}
		jobH = h
	})
	return jobH, jobErr
}

// prepareChild has no pre-start work on Windows.
func prepareChild(*exec.Cmd) {}

// trackChild assigns the started kernel to the kill-on-close job object.
func trackChild(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	job, err := ensureJob()
	if err != nil {
		return err
	}
	ph, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(ph)
	if err := windows.AssignProcessToJobObject(job, ph); err != nil {
		return fmt.Errorf("assign to job: %w", err)
	}
	return nil
}
