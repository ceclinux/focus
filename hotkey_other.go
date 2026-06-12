//go:build !darwin

package main

import "fmt"

func runHotkeyDaemon() error {
	return fmt.Errorf("global hotkey daemon is currently implemented only on macOS")
}
