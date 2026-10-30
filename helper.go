package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func waitForProcessExit(pid uint32, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		// The process already exited between spawn and OpenProcess.
		if err == windows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	milliseconds := uint32(timeout / time.Millisecond)
	if timeout <= 0 {
		milliseconds = windows.INFINITE
	}
	result, err := windows.WaitForSingleObject(handle, milliseconds)
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("process %d did not exit in time", pid)
	}
	return nil
}

func runUpdateHelper(parentPID uint32, targetPath, newVersion string) error {
	selfPath, err := os.Executable()
	if err != nil {
		return err
	}
	defer scheduleSelfDelete(selfPath)
	if err := waitForProcessExit(parentPID, 60*time.Second); err != nil {
		return err
	}

	dir := filepath.Dir(targetPath)
	backup := filepath.Join(dir, "bjp-agent.previous.exe")
	healthy := filepath.Join(dir, "healthy.version")
	failed := filepath.Join(dir, "update.failed")
	_ = os.Remove(healthy)
	_ = os.Remove(backup)
	if err := copyFile(targetPath, backup); err != nil {
		return fmt.Errorf("backup current version: %w", err)
	}
	if err := copyFileWithRetry(selfPath, targetPath); err != nil {
		return fmt.Errorf("install update: %w", err)
	}

	cmd := exec.Command(targetPath)
	cmd.SysProcAttr = hiddenProcessAttributes()
	if err := cmd.Start(); err != nil {
		_ = copyFileWithRetry(backup, targetPath)
		return fmt.Errorf("start update: %w", err)
	}

	if waitForHealthyVersion(healthy, newVersion, 30*time.Second) {
		_ = os.Remove(backup)
		_ = os.Remove(failed)
		return nil
	}

	_ = cmd.Process.Kill()
	_ = waitForProcessExit(uint32(cmd.Process.Pid), 10*time.Second)
	if err := copyFileWithRetry(backup, targetPath); err != nil {
		return fmt.Errorf("rollback copy: %w", err)
	}
	_ = os.WriteFile(failed, []byte(newVersion+"|"+strconv.FormatInt(time.Now().Unix(), 10)), 0o600)
	rollback := exec.Command(targetPath)
	rollback.SysProcAttr = hiddenProcessAttributes()
	return rollback.Start()
}

func waitForHealthyVersion(path, version string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == version {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func startCleanupHelper(parentPID uint32, targetDir string) error {
	selfPath, err := os.Executable()
	if err != nil {
		return err
	}
	tempPath := filepath.Join(os.TempDir(), "bjp-cleanup-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".exe")
	if err := copyFile(selfPath, tempPath); err != nil {
		return err
	}
	cmd := exec.Command(tempPath,
		"--internal-cleanup",
		"--parent-pid", strconv.FormatUint(uint64(parentPID), 10),
		"--target-dir", targetDir,
	)
	cmd.SysProcAttr = hiddenProcessAttributes()
	return cmd.Start()
}

func runCleanupHelper(parentPID uint32, targetDir string) error {
	selfPath, _ := os.Executable()
	defer scheduleSelfDelete(selfPath)
	_ = waitForProcessExit(parentPID, 30*time.Second)
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		lastErr = os.RemoveAll(targetDir)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(350 * time.Millisecond)
	}
	return lastErr
}

func requestFileDelete(path string) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(installDir(), "delete-launcher.path"), []byte(path), 0o600)
}

func cleanupRequestedFile(ctx context.Context) {
	requestPath := filepath.Join(installDir(), "delete-launcher.path")
	data, err := os.ReadFile(requestPath)
	if err != nil {
		return
	}
	target := string(data)
	for attempt := 0; attempt < 30; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(350 * time.Millisecond):
		}
		if err := os.Remove(target); err == nil || os.IsNotExist(err) {
			_ = os.Remove(requestPath)
			return
		}
	}
}

func cleanupStaleUpdateFiles(ctx context.Context) {
	select {
	case <-time.After(8 * time.Second):
	case <-ctx.Done():
		return
	}
	for _, name := range []string{"bjp-agent.update.exe", "bjp-agent.update.exe.download"} {
		_ = os.Remove(filepath.Join(installDir(), name))
	}
}

func scheduleSelfDelete(path string) {
	from, err := windows.UTF16PtrFromString(path)
	if err == nil {
		_ = windows.MoveFileEx(from, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
}

func hiddenProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
