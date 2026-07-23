//go:build windows

package elevate

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// IsAdmin returns true if the current process has administrator privileges.
func IsAdmin() bool {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	isUserAnAdmin := shell32.NewProc("IsUserAnAdmin")
	ret, _, _ := isUserAnAdmin.Call()
	return ret != 0
}

// RunElevated re-launches the current executable with UAC elevation.
// It uses ShellExecuteW with the "runas" verb to trigger the UAC prompt.
func RunElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteW := shell32.NewProc("ShellExecuteW")

	// Build command line arguments
	var params string
	for _, arg := range args {
		params += " " + syscall.EscapeArg(arg)
	}
	params = strings.TrimSpace(params)

	// ShellExecuteW(NULL, "runas", exe, params, NULL, SW_SHOWNORMAL)
	// Parameters:
	//   hwnd         - handle to parent window (0 = no parent)
	//   lpOperation  - "runas" to trigger UAC elevation
	//   lpFile       - executable path
	//   lpParameters - command line arguments
	//   lpDirectory  - working directory (0 = use exe's directory)
	//   nShowCmd     - SW_SHOWNORMAL (1)
	ret, _, err := shellExecuteW.Call(
		0, // hwnd
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("runas"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(exe))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(params))),
		0, // directory
		syscall.SW_SHOWNORMAL,
	)

	// ShellExecuteW returns > 32 on success, <= 32 on failure
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW failed (code %d): %v", ret, err)
	}

	return nil
}
