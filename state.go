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
	return os.WriteFile(path, []byte(fmt.Sprintf("%d %d\n", state.FocusPID, state.KewPID)), 0o644)
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
	return playbackState{FocusPID: focusPID, KewPID: kewPID}, nil
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

func stopExistingKewIfAny() (bool, error) {
	pid, err := findExistingKewPID()
	if err != nil {
		return false, err
	}
	if pid <= 0 {
		return false, nil
	}

	if !pidAlive(pid) {
		removePlaybackState()
		return false, nil
	}

	stopKew(pid)
	removePlaybackState()
	fmt.Fprintf(os.Stderr, "focus: stopped kew process %d\n", pid)
	return true, nil
}

func adoptExistingKew(deviceName string) (bool, error) {
	state, err := readPlaybackState()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		removePlaybackState()
		return false, nil
	}

	if state.KewPID <= 0 || !pidAlive(state.KewPID) {
		removePlaybackState()
		return false, nil
	}

	state.FocusPID = os.Getpid()
	if err := writePlaybackState(state); err != nil {
		fmt.Fprintf(os.Stderr, "focus: could not write playback state: %v\n", err)
	}

	monitorKewUntilDisconnect(state.KewPID, deviceName)
	return true, nil
}

func getProcessName(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid")
	}
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func stopKew(pid int) {
	if pid <= 0 {
		return
	}
	name, err := getProcessName(pid)
	if err != nil || !strings.Contains(name, "kew") {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
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

func monitorKewUntilDisconnect(pid int, deviceName string) {
	disconnectCh := make(chan error, 1)
	go monitorPlaybackDevice(deviceName, disconnectCh)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case err := <-disconnectCh:
			if err != nil {
				fmt.Fprintf(os.Stderr, "focus: playback device monitor error: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "focus: playback device %q disconnected\n", deviceName)
			}
			fmt.Fprintln(os.Stderr, "focus: stopping kew")
			stopKew(pid)
			removePlaybackState()
			return
		case <-ticker.C:
			if !pidAlive(pid) {
				removePlaybackState()
				return
			}
		}
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}