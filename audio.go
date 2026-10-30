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

// playWAV plays an in-memory WAV blob asynchronously via winmm.PlaySoundW.
// It respects the local rate limit (MinPlayGap) and quiet mode (microphone
// in use). Returns true if the sound actually started playing.
func playWAV(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	rateMu.Lock()
	if time.Since(lastPlay) < time.Duration(MinPlayGap)*time.Second {
		rateMu.Unlock()
		logf("skip: rate limit (%.1fs since last)", time.Since(lastPlay).Seconds())
		return false
	}
	if micInUse() {
		rateMu.Unlock()
		logf("skip: microphone in use (call detected)")
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
