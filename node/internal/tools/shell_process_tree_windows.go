//go:build windows

package tools

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsProcessTree struct {
	handle windows.Handle
}

func attachProcessTree(cmd *exec.Cmd) (processTreeHandle, error) {
	if cmd == nil || cmd.Process == nil {
		return nil, fmt.Errorf("shell process is not started")
	}

	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create Windows Job Object: %w", err)
	}
	job := &windowsProcessTree{handle: handle}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		job.close()
		return nil, fmt.Errorf("configure Windows Job Object: %w", err)
	}
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		job.close()
		return nil, fmt.Errorf("open shell process for Windows Job Object: %w", err)
	}
	defer windows.CloseHandle(processHandle)
	if err := windows.AssignProcessToJobObject(handle, processHandle); err != nil {
		job.close()
		return nil, fmt.Errorf("assign shell to Windows Job Object: %w", err)
	}
	return job, nil
}

func (j *windowsProcessTree) terminate() {
	if j == nil || j.handle == 0 {
		return
	}
	_ = windows.TerminateJobObject(j.handle, 1)
}

func (j *windowsProcessTree) close() {
	if j == nil || j.handle == 0 {
		return
	}
	_ = windows.CloseHandle(j.handle)
	j.handle = 0
}
