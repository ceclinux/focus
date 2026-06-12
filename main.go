package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	defaultDeviceName = "rcs app2"
	defaultKewQuery   = "Chill Rain Jazz.mp3"
)

func main() {
	installHotkey := flag.Bool("install-hotkey", false, "install Ctrl+Shift+Command+F global hotkey")
	uninstallHotkey := flag.Bool("uninstall-hotkey", false, "uninstall the global hotkey")
	hotkeyDaemon := flag.Bool("hotkey-daemon", false, "run the global hotkey daemon")
	kewNoUI := flag.Bool("noui", false, "pass --noui to kew")
	toggle := flag.Bool("toggle", false, "debounce hotkey repeats; existing kew playback is paused/resumed")
	deviceName := flag.String("device", getenvDefault("FOCUS_BT_DEVICE", defaultDeviceName), "paired Bluetooth device name to connect")
	defaultQuery := flag.String("query", getenvDefault("FOCUS_KEW_QUERY", defaultKewQuery), "kew search query to use when no positional query is supplied")
	flag.Parse()

	if *installHotkey {
		if err := installHotkeyLaunchAgent(); err != nil {
			fatal("could not install hotkey", err)
		}
		fmt.Fprintln(os.Stderr, "focus: installed Ctrl+Shift+Command+F hotkey")
		return
	}

	if *uninstallHotkey {
		if err := uninstallHotkeyLaunchAgent(); err != nil {
			fatal("could not uninstall hotkey", err)
		}
		fmt.Fprintln(os.Stderr, "focus: uninstalled Ctrl+Shift+Command+F hotkey")
		return
	}

	if *hotkeyDaemon {
		if err := runHotkeyDaemon(); err != nil {
			fatal("hotkey daemon failed", err)
		}
		return
	}

	targetOutputAvailableForToggle := true
	if *toggle {
		debounced, err := debounceToggle(750 * time.Millisecond)
		if err != nil {
			fmt.Fprintf(os.Stderr, "focus: could not update toggle debounce file: %v\n", err)
		} else if debounced {
			fmt.Fprintln(os.Stderr, "focus: ignored repeated hotkey event")
			return
		}

		available, err := isAudioOutputAvailable(*deviceName)
		if err != nil || !available {
			targetOutputAvailableForToggle = false
			fmt.Fprintf(os.Stderr, "focus: audio output %q is unavailable; connecting before touching kew\n", *deviceName)
		}
	}

	if !*toggle || targetOutputAvailableForToggle {
		toggled, err := toggleExistingKewIfAny()
		if err != nil {
			fatal("could not toggle existing kew process", err)
		}
		if toggled {
			return
		}
	}

	stateWritten := false
	if err := writePlaybackState(playbackState{FocusPID: os.Getpid()}); err != nil {
		fmt.Fprintf(os.Stderr, "focus: could not write playback startup state: %v\n", err)
	} else {
		stateWritten = true
	}

	kewArgs := flag.Args()
	if len(kewArgs) == 0 {
		kewArgs = strings.Fields(*defaultQuery)
	}
	if *kewNoUI {
		kewArgs = append([]string{"--noui"}, kewArgs...)
	}

	connectedName := *deviceName

	fmt.Fprintf(os.Stderr, "focus: checking audio output %q...\n", *deviceName)
	audioOutputName, err := setDefaultAudioOutputWithRetry(*deviceName, 2*time.Second)
	if err == nil {
		connectedName = audioOutputName
		fmt.Fprintf(os.Stderr, "focus: %q is already available; audio output set\n", connectedName)
	} else {
		audioErr := err
		alreadyConnected, btErr := isBluetoothDeviceConnected(*deviceName)
		if btErr == nil && alreadyConnected {
			fmt.Fprintf(os.Stderr, "focus: Bluetooth device %q is already connected\n", connectedName)
		} else {
			fmt.Fprintf(os.Stderr, "focus: connecting to Bluetooth device %q...\n", *deviceName)
			connectedName, err = connectBluetoothDevice(*deviceName)
			if err != nil {
				fatal("could not find an existing audio output or connect to Bluetooth device", fmt.Errorf("audio output check: %v; Bluetooth connect: %w", audioErr, err))
			}
			fmt.Fprintf(os.Stderr, "focus: connected to %q\n", connectedName)
		}

		fmt.Fprintf(os.Stderr, "focus: routing audio output to %q...\n", connectedName)
		audioOutputName, err = setDefaultAudioOutputWithRetry(connectedName, 10*time.Second)
		if err != nil {
			fatal("could not route audio to Bluetooth device", err)
		}
		connectedName = audioOutputName
		fmt.Fprintf(os.Stderr, "focus: audio output set to %q\n", connectedName)
	}

	if *toggle && !targetOutputAvailableForToggle {
		resumed, err := resumeExistingKewIfAny(connectedName)
		if err != nil {
			fatal("could not resume existing kew process", err)
		}
		if resumed {
			return
		}
	}

	if _, err := exec.LookPath("kew"); err != nil {
		fatal("kew is not installed or not in PATH", err)
	}

	fmt.Fprintf(os.Stderr, "focus: starting kew %q\n", strings.Join(kewArgs, " "))
	cmd := exec.Command("kew", kewArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fatal("could not start kew", err)
	}
	if err := writePlaybackState(playbackState{FocusPID: os.Getpid(), KewPID: cmd.Process.Pid}); err != nil {
		fmt.Fprintf(os.Stderr, "focus: could not write playback state: %v\n", err)
	} else {
		stateWritten = true
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	disconnectCh := make(chan error, 1)
	go monitorPlaybackDevice(connectedName, disconnectCh)

	select {
	case err := <-waitCh:
		if stateWritten {
			removePlaybackState()
		}
		handleKewExit(err)
	case err := <-disconnectCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "focus: playback device monitor error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "focus: playback device %q disconnected\n", connectedName)
		}
		fmt.Fprintln(os.Stderr, "focus: pausing kew")
		pauseKew(cmd.Process.Pid)
		if err := writePlaybackState(playbackState{KewPID: cmd.Process.Pid, Paused: true}); err != nil {
			fmt.Fprintf(os.Stderr, "focus: could not write paused playback state: %v\n", err)
		}
	}
}

func setDefaultAudioOutputWithRetry(deviceName string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for {
		outputName, err := setDefaultAudioOutput(deviceName)
		if err == nil {
			return outputName, nil
		}
		lastErr = err

		if time.Now().After(deadline) {
			return "", lastErr
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func monitorPlaybackDevice(deviceName string, disconnectCh chan<- error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		available, audioErr := isAudioOutputAvailable(deviceName)
		if audioErr == nil {
			if !available {
				disconnectCh <- nil
				return
			}
			continue
		}

		connected, bluetoothErr := isBluetoothDeviceConnected(deviceName)
		if bluetoothErr != nil {
			disconnectCh <- fmt.Errorf("audio output check failed: %v; Bluetooth check failed: %v", audioErr, bluetoothErr)
			return
		}
		if !connected {
			disconnectCh <- nil
			return
		}
	}
}

func handleKewExit(err error) {
	if err == nil {
		return
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	fatal("kew failed", err)
}

func getenvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func fatal(message string, err error) {
	fmt.Fprintf(os.Stderr, "focus: %s: %v\n", message, err)
	os.Exit(1)
}
