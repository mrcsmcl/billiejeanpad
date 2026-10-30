package main

import (
	"golang.org/x/sys/windows/registry"
)

// Windows 10/11 writes microphone usage events here. Each subkey is one
// app; packaged apps sit directly, desktop apps live under NonPackaged.
// Values LastUsedTimeStart/Stop are FILETIME; Start > Stop means that
// app is currently capturing audio.
const micRegPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone`

// micInUse returns true if any app currently has the microphone open.
// Used by playWAV to engage quiet mode during calls.
func micInUse() bool {
	return scanMic(micRegPath)
}

func scanMic(path string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return false
	}
	defer k.Close()

	subs, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return false
	}

	for _, sub := range subs {
		child := path + `\` + sub
		if sub == "NonPackaged" {
			if scanMic(child) {
				return true
			}
			continue
		}

		ck, err := registry.OpenKey(registry.CURRENT_USER, child, registry.READ)
		if err != nil {
			continue
		}
		start, _, _ := ck.GetIntegerValue("LastUsedTimeStart")
		stop, _, _ := ck.GetIntegerValue("LastUsedTimeStop")
		ck.Close()

		// Start recorded and no matching stop yet (or stop is older) =
		// mic is still open.
		if start != 0 && stop < start {
			return true
		}
	}
	return false
}
