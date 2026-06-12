package main

/*
#cgo darwin LDFLAGS: -framework Carbon
#include <stdlib.h>

int FocusRunHotkeyLoop(char **errorMessage);
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"
)

var hotkeyProcess struct {
	sync.Mutex
	running bool
}

func runHotkeyDaemon() error {
	fmt.Fprintln(os.Stderr, "focus: hotkey daemon started; press Ctrl+Shift+Command+F")

	var cError *C.char
	status := C.FocusRunHotkeyLoop(&cError)
	if status != 0 {
		return cgoError(cError, "could not register hotkey")
	}
	return nil
}

//export FocusHotkeyPressed
func FocusHotkeyPressed() {
	go launchFocusFromHotkey()
}

func launchFocusFromHotkey() {
	hotkeyProcess.Lock()
	if hotkeyProcess.running {
		hotkeyProcess.Unlock()
		fmt.Fprintln(os.Stderr, "focus: hotkey ignored; focus is already running")
		return
	}
	hotkeyProcess.running = true
	hotkeyProcess.Unlock()

	defer func() {
		hotkeyProcess.Lock()
		hotkeyProcess.running = false
		hotkeyProcess.Unlock()
	}()

	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "focus: hotkey could not find executable: %v\n", err)
		return
	}

	cmd := exec.Command(executable, "-toggle", "-noui")
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withHotkeyPath(os.Environ())

	fmt.Fprintln(os.Stderr, "focus: hotkey pressed; starting playback")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "focus: hotkey playback exited: %v\n", err)
	}
}

func withHotkeyPath(env []string) []string {
	const path = "PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	for i, value := range env {
		if len(value) >= 5 && value[:5] == "PATH=" {
			env[i] = path
			return env
		}
	}
	return append(env, path)
}

func cStringError(cError *C.char, fallback string) error {
	if cError == nil {
		return fmt.Errorf("%s", fallback)
	}
	defer C.free(unsafe.Pointer(cError))
	return fmt.Errorf("%s", C.GoString(cError))
}
