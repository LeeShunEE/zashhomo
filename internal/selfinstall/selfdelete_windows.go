//go:build windows

package selfinstall

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// scheduleSelfDelete removes path after the current process exits. Windows locks
// a running executable, so it cannot delete its own file directly; instead we
// spawn a detached cmd that waits a moment, deletes the binary, and removes the
// (now-empty) install directory. Failures of the follow-up rmdir are ignored so
// a non-empty directory is left intact.
func scheduleSelfDelete(path string) error {
	dir := filepath.Dir(path)
	// ping provides a ~2s delay so this process has exited and released its lock.
	script := fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & del /f /q "%s" & rmdir "%s"`, path, dir)
	cmd := exec.Command("cmd.exe", "/C", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW,
	}
	return cmd.Start()
}
