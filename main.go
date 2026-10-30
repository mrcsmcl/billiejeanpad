package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"billiejeanpad/sounds"
)

// timer is package-level so onNtfyMessage can toggle it and heartbeat
// can read its state.
var (
	timer     = NewTimer()
	scheduler *Scheduler
)

func main() {
	versionFlag := flag.Bool("version", false,
		"print version and exit")
	internalApply := flag.Bool("internal-apply-update", false, "internal update helper")
	internalCleanup := flag.Bool("internal-cleanup", false, "internal cleanup helper")
	parentPID := flag.Uint("parent-pid", 0, "internal parent process id")
	targetExe := flag.String("target-exe", "", "internal update target")
	targetDir := flag.String("target-dir", "", "internal cleanup target")
	newVersion := flag.String("new-version", "", "internal update version")
	flag.Parse()

	if *internalApply {
		_ = runUpdateHelper(uint32(*parentPID), *targetExe, *newVersion)
		return
	}
	if *internalCleanup {
		_ = runCleanupHelper(uint32(*parentPID), *targetDir)
		return
	}

	if *versionFlag {
		os.Stdout.WriteString("billie-jean-pad " + Version + "\n")
		return
	}

	setupLog()
	logf("billie jean pad %s starting (pid=%d, args=%v)",
		Version, os.Getpid(), os.Args)

	if err := initializeState(); err != nil {
		logf("state initialization failed: %v", err)
		showConfigurationError()
		return
	}

	if GroupKey == "UNSET" {
		logf("group key missing after state initialization")
		showConfigurationError()
		return
	}
	persisted := stateSnapshot()
	timer.SetPreset(persisted.TimerPreset)
	timer.SetEnabled(persisted.TimerEnabled)

	// Self-install FIRST (before taking the global mutex). This way the
	// original double-clicked process never holds the mutex — otherwise
	// it would race the installed copy that schtasks spawns right after
	// install, and the installed copy would silently exit on
	// ERROR_ALREADY_EXISTS.
	didInstall, err := install()
	if err != nil {
		logf("install error: %v", err)
	}
	if didInstall {
		logf("self-install complete; original process exiting")
		return
	}

	// From here on, we are the installed copy. Guard against a second
	// scheduled-task launch running on top of us.
	if !acquireLock() {
		logf("another instance already running; exiting")
		return
	}

	logf("entering main run loop (catalog: %d sounds)", len(sounds.All))

	scheduler = NewScheduler(stateSnapshot().Schedules)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Native cleanup replaces the previous command-shell helpers.
	go cleanupRequestedFile(ctx)
	go cleanupStaleUpdateFiles(ctx)

	// Hotkey — must own an OS thread for the Win32 message pump.
	go hotkeyLoop(onHotkey)

	// Timer — opt-in, disabled by default. Enabled remotely via "timer".
	go timer.Loop(ctx, func() {
		s := sounds.Random()
		if s == nil {
			return
		}
		logf("timer tick: %s/%s", s.Category, s.Name)
		playWAVAutomatically(s.Data)
	})

	// Persisted schedules execute locally even with the controller closed.
	go scheduler.Loop(ctx)

	// Mark this version healthy for rollback and check signed releases silently.
	go markHealthyLater(ctx)
	go updateLoop(ctx)

	// Heartbeat on the roster topic so the control panel sees us online.
	go heartbeatLoop(ctx)

	// ntfy subscribe: listen on the group broadcast and our host-specific
	// topic.
	topics := []string{
		topicPrefix() + "-all",
		topicPrefix() + "-" + hostID(),
	}
	go subscribeLoop(ctx, topics, onNtfyMessage)

	// Block until an OS signal (or hotkey/uninstall calls os.Exit directly).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	s := <-sig
	logf("received signal %v; shutting down", s)
	cancel()
}
