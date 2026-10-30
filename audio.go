package main

import (
	"sync"
	"time"
	"unsafe"
)

const (
	sndASYNC     = 0x0001
	sndMEMORY    = 0x0004
	sndNODEFAULT = 0x0002
)

var (
	rateMu   sync.Mutex
	lastPlay time.Time
)

// playWAV plays a manual command. Explicit operator actions are allowed even
// during calls; only automatic timer events use quiet mode.
func playWAV(data []byte) bool {
	return playWAVWithQuietMode(data, false)
}

// playWAVAutomatically suppresses automatic sounds while the microphone is
// active so a random timer event does not interrupt a call.
func playWAVAutomatically(data []byte) bool {
	return playWAVWithQuietMode(data, true)
}

func playWAVWithQuietMode(data []byte, respectMicrophone bool) bool {
	if len(data) == 0 {
		return false
	}

	rateMu.Lock()
	if time.Since(lastPlay) < time.Duration(MinPlayGap)*time.Second {
		rateMu.Unlock()
		logf("skip: rate limit (%.1fs since last)", time.Since(lastPlay).Seconds())
		return false
	}
	if respectMicrophone && micInUse() {
		rateMu.Unlock()
		logf("skip automatic sound: microphone in use")
		return false
	}
	lastPlay = time.Now()
	rateMu.Unlock()

	r, _, err := procPlaySoundW.Call(
		uintptr(unsafe.Pointer(&data[0])),
		0,
		uintptr(sndASYNC|sndMEMORY|sndNODEFAULT),
	)
	if r == 0 {
		logf("PlaySound failed: %v", err)
		return false
	}
	return true
}

// stopAudio cancels any sound currently playing.
func stopAudio() {
	procPlaySoundW.Call(0, 0, 0)
}
