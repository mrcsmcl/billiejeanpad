package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	mbOK              = 0x00000000
	mbICONINFORMATION = 0x00000040

	errorAlreadyExists = 183
	createNoWindow     = 0x08000000
)

// taskXML is the Scheduled Task definition. Registered under the current
// user, LeastPrivilege, runs at logon, keeps running on battery.
const taskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Billie Jean Pad background runner</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>__EXE_PATH__</Command>
    </Exec>
  </Actions>
</Task>`

func installDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), AppName)
}

func installExePath() string {
	return filepath.Join(installDir(), InstalledExeName)
}

// install performs first-run setup if the running binary is not the
// installed copy. On success returns (installed=true, nil); the caller
// should exit because the scheduled task now runs the installed copy.
// If the binary is already running from the install location, returns
// (false, nil) and the caller continues to the main loop.
func install() (installed bool, err error) {
	selfPath, err := os.Executable()
	if err != nil {
		return false, err
	}

	alreadyInstalled, _ := sameFile(selfPath, installExePath())
	if alreadyInstalled {
		return false, nil
	}

	logf("self-install starting (self=%s)", selfPath)

	if err := os.MkdirAll(installDir(), 0o755); err != nil {
		return false, fmt.Errorf("mkdir: %w", err)
	}
	legacyPath := filepath.Join(installDir(), LegacyExeName)
	_, currentErr := os.Stat(installExePath())
	_, legacyErr := os.Stat(legacyPath)
	if currentErr == nil || legacyErr == nil {
		logf("existing installation found; stopping it before update")
		for _, image := range []string{InstalledExeName, LegacyExeName} {
			stopCmd := exec.Command("taskkill", "/f", "/im", image)
			stopCmd.SysProcAttr = hiddenProcessAttributes()
			_ = stopCmd.Run()
		}
		time.Sleep(500 * time.Millisecond)
		_ = os.Remove(legacyPath)
	}
	if err := copyFileWithRetry(selfPath, installExePath()); err != nil {
		return false, fmt.Errorf("copy exe: %w", err)
	}
	logf("copied to %s", installExePath())

	xmlBody := strings.ReplaceAll(taskXML, "__EXE_PATH__", installExePath())
	tmpXML := filepath.Join(os.TempDir(), "billie-jean-pad-task.xml")
	if err := writeUTF16(tmpXML, xmlBody); err != nil {
		return false, fmt.Errorf("write xml: %w", err)
	}
	defer os.Remove(tmpXML)

	regCmd := exec.Command("schtasks", "/create", "/tn", AppName,
		"/xml", tmpXML, "/f")
	regCmd.SysProcAttr = hiddenProcessAttributes()
	taskRegistered := true
	if out, taskErr := regCmd.CombinedOutput(); taskErr != nil {
		taskRegistered = false
		logf("scheduled task unavailable (%v: %s); using HKCU Run fallback", taskErr, out)
		if err := registerRunFallback(); err != nil {
			return false, fmt.Errorf("startup registration failed (task: %v, Run: %w)", taskErr, err)
		}
		logf("HKCU Run fallback registered")
	} else {
		logf("scheduled task %q registered", AppName)
		// Remove a stale fallback left by an earlier install.
		_ = removeRunFallback()
	}

	if taskRegistered {
		runCmd := exec.Command("schtasks", "/run", "/tn", AppName)
		runCmd.SysProcAttr = hiddenProcessAttributes()
		if err := runCmd.Run(); err != nil {
			logf("schtasks run failed; launching directly: %v", err)
			if err := startInstalledCopy(); err != nil {
				return false, fmt.Errorf("start installed copy: %w", err)
			}
		}
	} else if err := startInstalledCopy(); err != nil {
		return false, fmt.Errorf("start installed copy: %w", err)
	}

	return true, nil
}

func registerRunFallback() error {
	key, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE,
	)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(AppName, `"`+installExePath()+`"`)
}

func removeRunFallback() error {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE,
	)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(AppName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func startInstalledCopy() error {
	cmd := exec.Command(installExePath())
	cmd.SysProcAttr = hiddenProcessAttributes()
	return cmd.Start()
}

// uninstall removes startup registration and asks a hidden PowerShell
// helper to stop remaining instances and retry deletion of the install
// directory. No visible cmd window or temporary batch file is used.
func uninstall() {
	stopAudio()

	delCmd := exec.Command("schtasks", "/delete", "/tn", AppName, "/f")
	delCmd.SysProcAttr = hiddenProcessAttributes()
	if out, err := delCmd.CombinedOutput(); err != nil {
		logf("schtasks delete (continuing): %v: %s", err, out)
	}
	if err := removeRunFallback(); err != nil {
		logf("HKCU Run cleanup (continuing): %v", err)
	}

	dir := quotePowerShellLiteral(installDir())
	script := fmt.Sprintf(`$target=%s; Start-Sleep -Milliseconds 350; Get-Process -Name 'BJPAgent','billiejeanpad' -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue; for ($i=0; $i -lt 20; $i++) { Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue; if (-not (Test-Path -LiteralPath $target)) { break }; Start-Sleep -Milliseconds 350 }`, dir)
	if err := startHiddenPowerShell(script); err != nil {
		logf("uninstall cleanup helper failed: %v", err)
	}
	os.Exit(0)
}

// showConfigurationError is only used for a developer mistake: running a
// binary built without the private group key.
func showConfigurationError() {
	titlePtr, _ := syscall.UTF16PtrFromString("Billie Jean Pad — Build incompleto")
	bodyPtr, _ := syscall.UTF16PtrFromString(
		"Este executável não contém a configuração do grupo.\r\n\r\n" +
			"Gere o aplicativo com scripts\\build.ps1 e tente novamente.",
	)
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(bodyPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbOK|mbICONINFORMATION),
	)
}

// acquireLock creates a named Windows mutex. Returns true if we are the
// first instance, false if another instance already holds it.
func acquireLock() bool {
	namePtr, _ := syscall.UTF16PtrFromString(MutexName)
	r, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if r == 0 {
		logf("CreateMutex failed: %v", callErr)
		return false
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == syscall.Errno(errorAlreadyExists) {
		return false
	}
	return true
}

func scheduleLauncherDelete() error {
	selfPath, err := os.Executable()
	if err != nil {
		return err
	}
	isInstalled, _ := sameFile(selfPath, installExePath())
	if isInstalled {
		return nil
	}

	target := quotePowerShellLiteral(selfPath)
	script := fmt.Sprintf(`$target=%s; Wait-Process -Id %d -ErrorAction SilentlyContinue; for ($i=0; $i -lt 20; $i++) { Remove-Item -LiteralPath $target -Force -ErrorAction SilentlyContinue; if (-not (Test-Path -LiteralPath $target)) { break }; Start-Sleep -Milliseconds 350 }`, target, os.Getpid())
	return startHiddenPowerShell(script)
}

func hiddenProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

func quotePowerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func startHiddenPowerShell(script string) error {
	cmd := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-Command", script,
	)
	cmd.SysProcAttr = hiddenProcessAttributes()
	return cmd.Start()
}

// --- helpers ---

// sameFile compares two paths after normalization. Case-insensitive on
// Windows.
func sameFile(a, b string) (bool, error) {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(aa, bb), nil
}

func copyFileWithRetry(src, dst string) error {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		if err := copyFile(src, dst); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return lastErr
}

// copyFile does a straightforward byte-copy; good enough for our ~10MB
// executable.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// writeUTF16 writes text as UTF-16 LE with BOM. Required by schtasks
// /xml which rejects anything else.
func writeUTF16(path, text string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write([]byte{0xff, 0xfe}); err != nil {
		return err
	}
	codes := utf16.Encode([]rune(text))
	buf := make([]byte, len(codes)*2)
	for i, c := range codes {
		binary.LittleEndian.PutUint16(buf[i*2:], c)
	}
	_, err = f.Write(buf)
	return err
}
