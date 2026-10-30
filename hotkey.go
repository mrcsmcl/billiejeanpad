package main

import (
	"os"
	"runtime"
	"unsafe"
)

const (
	modCONTROL = 0x0002
	modSHIFT   = 0x0004
	vkF12      = 0x7B
	wmHOTKEY   = 0x0312
)

// winMSG matches the MSG struct in winuser.h.
type winMSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       [2]int32
	Reserved uint32
}

// hotkeyLoop registers Ctrl+Shift+F12 as a global hotkey and pumps
// messages. Must run on a locked OS thread because the hotkey is bound
// to the registering thread. Calls onPressed() each time the combo
// fires.
func hotkeyLoop(onPressed func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, err := procRegisterHotKey.Call(
		0, // hwnd 0 = thread-local hotkey
		1, // id
		uintptr(modCONTROL|modSHIFT),
		uintptr(vkF12),
	)
	if r == 0 {
		logf("RegisterHotKey failed: %v", err)
		return
	}
	logf("hotkey registered: Ctrl+Shift+F12")

	var m winMSG
	for {
		r, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)),
			0, 0, 0,
		)
		if int32(r) <= 0 {
			return
		}
		if m.Message == wmHOTKEY {
			onPressed()
		}
	}
}

// onHotkey is wired to the hotkey press. Stops any sound and exits.
// Startup registration will bring the app back at the next logon; permanent
// removal is available from the authenticated Machines tab.
func onHotkey() {
	stopAudio()
	logf("hotkey pressed; exiting")
	os.Exit(0)
}
