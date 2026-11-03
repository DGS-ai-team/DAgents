//go:build windows

package browser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Helpers reproduce the one-file bootloader -> worker -> browser hierarchy.
func TestBrowserJobHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--browser-job-helper" {
			separator = i
			break
		}
	}
	if separator < 0 {
		return
	}
	role, dir := os.Args[separator+1], os.Args[separator+2]
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if role == "owner" {
		log, err := os.Create(filepath.Join(dir, "owner.log"))
		if err != nil {
			t.Fatal(err)
		}
		stop, wait, err := startBrowserProcess(executable, browserJobHelperArgs("root", dir), dir, log)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		_ = wait()
		return
	}
	if role == "root" || role == "worker" {
		childRole := "worker"
		if role == "worker" {
			childRole = "leaf"
		}
		child := exec.Command(executable, browserJobHelperArgs(childRole, dir)...)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, role+".pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func browserJobHelperArgs(role, dir string) []string {
	return []string{"-test.run=^TestBrowserJobHelperProcess$", "--", "--browser-job-helper", role, dir}
}

func waitForBrowserJobPIDs(t *testing.T, dir string) []uint32 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var pids []uint32
		for _, role := range []string{"root", "worker", "leaf"} {
			raw, err := os.ReadFile(filepath.Join(dir, role+".pid"))
			if err != nil {
				break
			}
			pid, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
			if err != nil {
				break
			}
			pids = append(pids, uint32(pid))
		}
		if len(pids) == 3 {
			return pids
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("browser helper tree did not start in %s", dir)
	return nil
}

func browserTestProcessRunning(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && status == uint32(windows.WAIT_TIMEOUT)
}

func assertBrowserJobStopped(t *testing.T, pids []uint32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allStopped := true
		for _, pid := range pids {
			allStopped = allStopped && !browserTestProcessRunning(pid)
		}
		if allStopped {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("browser job left a live process: %v", pids)
}

func TestBrowserJobCloseStopsWholeTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "browser 空格 fixture")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	log, err := os.Create(filepath.Join(dir, "browser.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	stop, wait, err := startBrowserProcess(executable, browserJobHelperArgs("root", dir), dir, log)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	pids := waitForBrowserJobPIDs(t, dir)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	assertBrowserJobStopped(t, pids)
}

func TestBrowserJobOwnerTerminationStopsOnlyOwnedTree(t *testing.T) {
	dir := t.TempDir()
	executable, _ := os.Executable()
	controlDir := t.TempDir()
	control := exec.Command(executable, browserJobHelperArgs("control", controlDir)...)
	if err := control.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Process.Kill(); _ = control.Wait() }()
	owner := exec.Command(executable, browserJobHelperArgs("owner", dir)...)
	ownerLog, err := os.Create(filepath.Join(dir, "helper.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer ownerLog.Close()
	owner.Stdout, owner.Stderr = ownerLog, ownerLog
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	pids := waitForBrowserJobPIDs(t, dir)
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	assertBrowserJobStopped(t, pids)
	if !browserTestProcessRunning(uint32(control.Process.Pid)) {
		t.Fatal("stopping the browser owner also stopped an unrelated process")
	}
	t.Logf("forced owner termination reaped three descendants; control PID %d survived", control.Process.Pid)
}
