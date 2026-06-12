//go:build !darwin

package main

import "fmt"

func connectBluetoothDevice(targetName string) (string, error) {
	return "", fmt.Errorf("Bluetooth auto-connect is currently implemented only on macOS; cannot connect to %q on this OS", targetName)
}

func isBluetoothDeviceConnected(targetName string) (bool, error) {
	return false, fmt.Errorf("Bluetooth monitoring is currently implemented only on macOS; cannot monitor %q on this OS", targetName)
}

func setDefaultAudioOutput(targetName string) (string, error) {
	return "", fmt.Errorf("audio output routing is currently implemented only on macOS; cannot route audio to %q on this OS", targetName)
}

func isAudioOutputAvailable(targetName string) (bool, error) {
	return false, fmt.Errorf("audio output monitoring is currently implemented only on macOS; cannot monitor audio output %q on this OS", targetName)
}
