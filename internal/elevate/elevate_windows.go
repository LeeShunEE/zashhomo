//go:build windows

package elevate

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsAdmin returns true if the current process has administrator privileges.
func IsAdmin() bool {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	isUserAnAdmin := shell32.NewProc("IsUserAnAdmin")
	ret, _, _ := isUserAnAdmin.Call()
	return ret != 0
}

// shellExecuteInfo mirrors the Win32 SHELLEXECUTEINFOW structure. Field order
// and 64-bit padding match the C layout so cbSize == unsafe.Sizeof(sei).
type shellExecuteInfo struct {
	cbSize        uint32
	fMask         uint32
	hwnd          uintptr
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       uintptr
	idList        uintptr
	class         *uint16
	hkeyClass     uintptr
	hotKey        uint32
	iconOrMonitor uintptr
	process       uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040 // keep hProcess valid so we can wait on it
	swHide                = 0          // SW_HIDE: no visible window for the child
)

// RunElevated re-launches the current executable with UAC elevation via the
// "runas" verb. The elevated child runs hidden (no new console window); its
// stdout/stderr are redirected to a temp file (see ElevatedLogFlag). This
// process waits for the child to finish, prints its captured output in the
// current console, and returns the child's exit status.
func RunElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Temp file the elevated child writes its output to, which we relay here.
	logf, err := os.CreateTemp("", "zashhomo-elevated-*.log")
	if err != nil {
		return fmt.Errorf("create elevation log: %w", err)
	}
	logPath := logf.Name()
	logf.Close()
	defer os.Remove(logPath)

	// Append the private flag telling the child where to write its output.
	full := append(append([]string{}, args...), ElevatedLogFlag, logPath)
	var params string
	for _, a := range full {
		params += " " + syscall.EscapeArg(a)
	}
	params = strings.TrimSpace(params)

	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	paramsPtr, _ := syscall.UTF16PtrFromString(params)

	sei := shellExecuteInfo{
		fMask:      seeMaskNoCloseProcess,
		verb:       verbPtr,
		file:       exePtr,
		parameters: paramsPtr,
		show:       swHide,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	shellExecuteExW := shell32.NewProc("ShellExecuteExW")
	ret, _, callErr := shellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if ret == 0 {
		return fmt.Errorf("ShellExecuteExW failed: %v", callErr)
	}
	if sei.process == 0 {
		// Elevation started but we have no handle to wait on; best-effort return.
		return nil
	}

	h := windows.Handle(sei.process)
	defer windows.CloseHandle(h)

	// Wait for the elevated child to finish, then relay its captured output into
	// this (original) console so nothing flashes past in a separate window.
	_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
	relayed := false
	if out, rerr := os.ReadFile(logPath); rerr == nil && len(out) > 0 {
		os.Stdout.Write(out)
		relayed = true
	}

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != 0 {
		// The child already printed its own message; a wrapped "exited with
		// code N" line on top of it is redundant noise, so signal quietly.
		if relayed {
			return ErrChildReported
		}
		return fmt.Errorf("elevated operation exited with code %d", code)
	}
	return nil
}
