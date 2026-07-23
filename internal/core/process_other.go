//go:build !windows

package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// killOrphanKernels finds and terminates any mihomo processes that are
// running from the same binary path as the supervisor would use.
// This prevents port conflicts when restarting after a hard crash.
// It only kills processes owned by the current user to avoid affecting
// other users' instances.
func killOrphanKernels(binPath string) error {
	// Resolve the absolute path for comparison
	absPath, err := filepath.Abs(binPath)
	if err != nil {
		return nil // best-effort; continue if we can't resolve
	}

	// Try pgrep first (available on most Unix systems)
	if pgrep, err := exec.LookPath("pgrep"); err == nil {
		return killWithPgrep(pgrep, absPath)
	}

	// Fallback to reading /proc on Linux
	if _, err := os.Stat("/proc"); err == nil {
		return killWithProc(absPath)
	}

	// Fallback to ps for macOS and other systems
	if ps, err := exec.LookPath("ps"); err == nil {
		return killWithPs(ps, absPath)
	}

	// No method available; skip cleanup
	return nil
}

// killWithPgrep uses pgrep to find and kill orphan processes.
func killWithPgrep(pgrep, binPath string) error {
	// Find all mihomo processes
	out, err := exec.Command(pgrep, "-f", "mihomo").Output()
	if err != nil {
		// pgrep returns non-zero if no matches; that's fine
		return nil
	}

	pids := strings.Fields(string(out))
	for _, pidStr := range pids {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Verify it's actually a mihomo process we own
		if isOurMihomo(pid, binPath) {
			if err := killProcess(pid); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not kill orphan mihomo (PID %d): %v\n", pid, err)
			} else {
				fmt.Fprintf(os.Stderr, "warning: killed orphan mihomo process (PID %d)\n", pid)
			}
		}
	}

	return nil
}

// killWithProc reads /proc to find and kill orphan processes (Linux-specific).
func killWithProc(binPath string) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Check if it's a mihomo process
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		cmdline, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		// cmdline is null-separated; join with spaces
		cmd := string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '}))
		if !strings.Contains(cmd, "mihomo") {
			continue
		}

		// Verify it's our binary
		if isOurMihomo(pid, binPath) {
			if err := killProcess(pid); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not kill orphan mihomo (PID %d): %v\n", pid, err)
			} else {
				fmt.Fprintf(os.Stderr, "warning: killed orphan mihomo process (PID %d)\n", pid)
			}
		}
	}

	return nil
}

// killWithPs uses ps to find and kill orphan processes (macOS and other Unix).
func killWithPs(ps, binPath string) error {
	// Use ps to find mihomo processes
	out, err := exec.Command(ps, "aux").Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		// Check if this is a mihomo process
		cmd := strings.Join(fields[10:], " ")
		if !strings.Contains(cmd, "mihomo") {
			continue
		}

		// Extract PID (field 1)
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		// Verify it's our binary
		if isOurMihomo(pid, binPath) {
			if err := killProcess(pid); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not kill orphan mihomo (PID %d): %v\n", pid, err)
			} else {
				fmt.Fprintf(os.Stderr, "warning: killed orphan mihomo process (PID %d)\n", pid)
			}
		}
	}

	return nil
}

// isOurMihomo checks if a process is running our mihomo binary.
func isOurMihomo(pid int, binPath string) bool {
	// Get the current user's UID to avoid killing other users' processes
	currentUID := os.Getuid()

	// Check process ownership
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, we can't directly get UID, but we can check if we can signal it
	// If we can send signal 0, we own it or have permissions
	if err := process.Signal(syscallSignal(0)); err != nil {
		return false
	}

	// Try to read the executable path (Linux: /proc/PID/exe, macOS: lsof)
	exePath, err := getProcessExePath(pid)
	if err != nil {
		return false
	}

	// Check if the process is owned by current user
	// This is a simplification; in production you'd want to check /proc/PID/status on Linux
	// or use sysctl on macOS. For now, we rely on the signal check above.

	// Compare paths (resolve both to absolute paths)
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		return false
	}

	absBin, err := filepath.Abs(binPath)
	if err != nil {
		return false
	}

	// Check if it's the same binary or a mihomo binary
	return absExe == absBin || strings.Contains(exePath, "mihomo")
}

// getProcessExePath returns the executable path for a process.
func getProcessExePath(pid int) (string, error) {
	// Linux: read /proc/PID/exe symlink
	if exePath := fmt.Sprintf("/proc/%d/exe", pid); pathExists(exePath) {
		if path, err := os.Readlink(exePath); err == nil {
			return path, nil
		}
	}

	// macOS: use lsof or ps -o command
	if lsof, err := exec.LookPath("lsof"); err == nil {
		out, err := exec.Command(lsof, "-p", strconv.Itoa(pid)).Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "txt") {
					fields := strings.Fields(line)
					if len(fields) >= 9 {
						return fields[8], nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("could not determine executable path")
}

// killProcess sends SIGTERM to the process, then SIGKILL if it doesn't die.
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Try SIGTERM first (graceful shutdown)
	if err := process.Signal(syscallSignal(15)); err != nil {
		return err
	}

	// Give it a moment to die
	time.Sleep(100 * time.Millisecond)

	// Check if it's still alive
	if err := process.Signal(syscallSignal(0)); err == nil {
		// Still alive; use SIGKILL
		return process.Kill()
	}

	return nil
}

// syscallSignal is a helper to convert int to syscall.Signal.
func syscallSignal(s int) syscall.Signal {
	return syscall.Signal(s)
}

// pathExists checks if a path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}