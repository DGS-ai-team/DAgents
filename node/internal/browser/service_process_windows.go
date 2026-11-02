//go:build windows

package browser

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 10+: assign the child atomically at creation, before a PyInstaller
// bootloader can launch its worker. The non-inherited job handle belongs only
// to Node; even TerminateProcess(Node) then closes the entire browser tree.
// https://learn.microsoft.com/windows/win32/procthread/job-objects
const procThreadAttributeJobList = 0x0002000d

func startBrowserProcess(executable string, args []string, dir string, logFile *os.File) (stop func() error, wait func() error, err error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create browser job: %w", err)
	}
	started := false
	defer func() {
		if !started {
			_ = windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, nil, fmt.Errorf("configure browser job: %w", err)
	}
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, nil, err
	}
	defer attributes.Delete()
	if err := attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return nil, nil, fmt.Errorf("assign browser job: %w", err)
	}

	// Restrict inheritance to stdin and the browser log. In particular, the job
	// handle must not reach the child, or Node death would leave it open.
	var output windows.Handle
	current := windows.CurrentProcess()
	if err := windows.DuplicateHandle(current, windows.Handle(logFile.Fd()), current, &output, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(output)
	security := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	input, err := windows.CreateFile(windows.StringToUTF16Ptr("NUL"), windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &security, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, nil, err
	}
	defer windows.CloseHandle(input)
	handles := []windows.Handle{input, output}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return nil, nil, err
	}
	application, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return nil, nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{executable}, args...)))
	if err != nil {
		return nil, nil, err
	}
	directory, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, nil, err
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW,
			ShowWindow: windows.SW_HIDE, StdInput: input, StdOutput: output, StdErr: output,
		},
		ProcThreadAttributeList: attributes.List(),
	}
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(application, commandLine, nil, nil, true, flags, nil, directory, &startup.StartupInfo, &process); err != nil {
		return nil, nil, err
	}
	started = true
	_ = windows.CloseHandle(process.Thread)
	var once sync.Once
	var closeErr error
	closeJob := func() error {
		once.Do(func() { closeErr = windows.CloseHandle(job) })
		return closeErr
	}
	waitProcess := func() error {
		defer windows.CloseHandle(process.Process)
		defer closeJob()
		_, err := windows.WaitForSingleObject(process.Process, windows.INFINITE)
		return err
	}
	return closeJob, waitProcess, nil
}
