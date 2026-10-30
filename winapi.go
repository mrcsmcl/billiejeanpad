package main

import "syscall"

// Lazy-loaded Win32 DLLs and procs used across the app. Declared in one
// place so each file can reference them without redeclaring.
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procMessageBoxW    = user32.NewProc("MessageBoxW")
	procPlaySoundW     = winmm.NewProc("PlaySoundW")
	procRegisterHotKey = user32.NewProc("RegisterHotKey")
	procGetMessageW    = user32.NewProc("GetMessageW")
	procCreateMutexW   = kernel32.NewProc("CreateMutexW")
)
