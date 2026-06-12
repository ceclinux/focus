package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type playbackState struct {
	FocusPID int
	KewPID   int
	Paused   bool
}

func playbackStatePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "focus", "playback.pid"), nil
}

func lastTogglePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "focus", "last-toggle"), nil
}

func writePlaybackState(state playbackState) error {
	path, err := playbackStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	paused := 0
	if state.Paused {
		paused = 1
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d %d %d\n", state.FocusPID, state.KewPID, paused)), 0o644)
}

func removePlaybackState() {
	path, err := playbackStatePath()
	if err == nil {
		_ = os.Remove(path)
	}
}

func readPlaybackState() (playbackState, error) {
	path, err := playbackStatePath()
	if err != nil {
		return playbackState{}, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return playbackState{}, err
	}
	fields := strings.Fields(string(bytes))
	if len(fields) < 2 {
		return playbackState{}, fmt.Errorf("invalid playback state file %s", path)
	}
	focusPID, err := strconv.Atoi(fields[0])
	if err != nil {
		return playbackState{}, err
	}
	kewPID, err := strconv.Atoi(fields[1])
	if err != nil {
		return playbackState{}, err
	}
	paused := false
	if len(fields) >= 3 {
		paused = fields[2] == "1" || strings.EqualFold(fields[2], "true")
	}
	return playbackState{FocusPID: focusPID, KewPID: kewPID, Paused: paused}, nil
}

func debounceToggle(interval time.Duration) (bool, error) {
	path, err := lastTogglePath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}

	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < interval {
		return true, nil
	}
	return false, os.WriteFile(path, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o644)
}

func toggleExistingKewIfAny() (bool, error) {
	state, err := readPlaybackState()
	if err == nil {
		if state.KewPID > 0 && pidAlive(state.KewPID) {
			return true, toggleKewState(state)
		}
		if state.FocusPID > 0 && pidAlive(state.FocusPID) {
			fmt.Fprintln(os.Stderr, "focus: existing focus startup/playback is already in progress")
			return true, nil
		}
		removePlaybackState()
	} else if err != nil && !os.IsNotExist(err) {
		removePlaybackState()
	}

	pid, err := findExistingKewPID()
	if err != nil {
		return false, err
	}
	if pid <= 0 {
		return false, nil
	}

	state = playbackState{KewPID: pid, Paused: isPIDStopped(pid)}
	return true, toggleKewState(state)
}

func toggleKewState(state playbackState) error {
	paused := state.Paused || isPIDStopped(state.KewPID)
	if paused {
		resumeKew(state.KewPID)
		state.Paused = false
		fmt.Fprintf(os.Stderr, "focus: resumed kew process %d\n", state.KewPID)
	} else {
		pauseKew(state.KewPID)
		state.Paused = true
		fmt.Fprintf(os.Stderr, "focus: paused kew process %d\n", state.KewPID)
	}
	return writePlaybackState(state)
}

func pauseKew(pid int) {
	if pid <= 0 {
		return
	}
	originalVolume, restoreVolume := fadeOutSystemVolume(1200*time.Millisecond, 12)
	if err := syscall.Kill(-pid, syscall.SIGSTOP); err != nil {
		_ = syscall.Kill(pid, syscall.SIGSTOP)
	}
	if restoreVolume {
		_ = setSystemOutputVolume(originalVolume)
	}
}

func resumeKew(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGCONT); err != nil {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
}

func fadeOutSystemVolume(duration time.Duration, steps int) (int, bool) {
	if steps <= 0 {
		return 0, false
	}

	original, err := getSystemOutputVolume()
	if err != nil || original <= 0 {
		return 0, false
	}

	sleep := duration / time.Duration(steps)
	for step := steps - 1; step >= 0; step-- {
		volume := original * step / steps
		_ = setSystemOutputVolume(volume)
		time.Sleep(sleep)
	}

	return original, true
}

func getSystemOutputVolume() (int, error) {
	output, err := exec.Command("osascript", "-e", "output volume of (get volume settings)").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(output)))
}

func setSystemOutputVolume(volume int) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	return exec.Command("osascript", "-e", fmt.Sprintf("set volume output volume %d", volume)).Run()
}

func findExistingKewPID() (int, error) {
	output, err := exec.Command("pgrep", "-x", "kew").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0, nil
		}
		return 0, err
	}
	for _, field := range strings.Fields(string(output)) {
		pid, err := strconv.Atoi(field)
		if err == nil && pidAlive(pid) {
			return pid, nil
		}
	}
	return 0, nil
}

func isPIDStopped(pid int) bool {
	if pid <= 0 {
		return false
	}
	output, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	state := strings.TrimSpace(string(output))
	return strings.HasPrefix(state, "T")
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
