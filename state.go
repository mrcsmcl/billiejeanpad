package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cryptProtectUIForbidden = 0x1

type appState struct {
	GroupKey     string     `json:"groupKey"`
	TimerEnabled bool       `json:"timerEnabled"`
	TimerPreset  string     `json:"timerPreset"`
	Schedules    []Schedule `json:"schedules,omitempty"`
}

var (
	stateMu      sync.RWMutex
	currentState appState
)

func statePath() string {
	return filepath.Join(installDir(), "state.dat")
}

// initializeState migrates the group key embedded in manual v3 builds into a
// DPAPI-protected local file. Generic update binaries have GroupKey=UNSET and
// recover the key from this file instead.
func initializeState() error {
	stateMu.Lock()
	defer stateMu.Unlock()

	loaded, err := readProtectedState()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read state: %w", err)
	}
	if loaded != nil {
		currentState = *loaded
	}
	if currentState.TimerPreset == "" {
		currentState.TimerPreset = "discreto"
	}

	// A bootstrap Share/Keep build may intentionally move the machine to a
	// new group. A generic updater never overrides local state.
	if GroupKey != "UNSET" && currentState.GroupKey != GroupKey {
		currentState.GroupKey = GroupKey
		currentState.Schedules = nil
	}
	if currentState.GroupKey == "" {
		return errors.New("no group key available")
	}
	GroupKey = currentState.GroupKey
	return writeProtectedStateLocked()
}

func stateSnapshot() appState {
	stateMu.RLock()
	defer stateMu.RUnlock()
	copyState := currentState
	copyState.Schedules = append([]Schedule(nil), currentState.Schedules...)
	return copyState
}

func updateTimerState(enabled bool, preset string) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	currentState.TimerEnabled = enabled
	if preset != "" {
		currentState.TimerPreset = preset
	}
	return writeProtectedStateLocked()
}

func replaceSchedules(schedules []Schedule) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	currentState.Schedules = append([]Schedule(nil), schedules...)
	return writeProtectedStateLocked()
}

func readProtectedState() (*appState, error) {
	protected, err := os.ReadFile(statePath())
	if err != nil {
		return nil, err
	}
	plain, err := unprotectData(protected)
	if err != nil {
		return nil, err
	}
	var state appState
	if err := json.Unmarshal(plain, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeProtectedStateLocked() error {
	if err := os.MkdirAll(installDir(), 0o700); err != nil {
		return err
	}
	plain, err := json.Marshal(currentState)
	if err != nil {
		return err
	}
	protected, err := protectData(plain)
	if err != nil {
		return err
	}
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, protected, 0o600); err != nil {
		return err
	}
	from, _ := windows.UTF16PtrFromString(tmp)
	to, _ := windows.UTF16PtrFromString(statePath())
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func protectData(data []byte) ([]byte, error) {
	in := bytesToBlob(data)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data))))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

func unprotectData(data []byte) ([]byte, error) {
	in := bytesToBlob(data)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data))))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

func bytesToBlob(data []byte) windows.DataBlob {
	blob := windows.DataBlob{Size: uint32(len(data))}
	if len(data) > 0 {
		blob.Data = &data[0]
	}
	return blob
}
