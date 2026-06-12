package main

/*
#cgo darwin CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo darwin LDFLAGS: -framework Foundation -framework IOBluetooth -framework CoreAudio
#include <stdlib.h>

char *FocusConnectBluetoothDevice(const char *targetName, char **errorMessage);
char *FocusSetDefaultAudioOutputDevice(const char *targetName, char **errorMessage);
int FocusIsAudioOutputDeviceAvailable(const char *targetName, char **errorMessage);
int FocusIsBluetoothDeviceConnected(const char *targetName, char **errorMessage);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func connectBluetoothDevice(targetName string) (string, error) {
	cTargetName := C.CString(targetName)
	defer C.free(unsafe.Pointer(cTargetName))

	var cError *C.char
	cConnectedName := C.FocusConnectBluetoothDevice(cTargetName, &cError)
	if cConnectedName == nil {
		return "", cgoError(cError, "unknown Bluetooth error")
	}
	defer C.free(unsafe.Pointer(cConnectedName))

	return C.GoString(cConnectedName), nil
}

func isBluetoothDeviceConnected(targetName string) (bool, error) {
	cTargetName := C.CString(targetName)
	defer C.free(unsafe.Pointer(cTargetName))

	var cError *C.char
	connected := C.FocusIsBluetoothDeviceConnected(cTargetName, &cError)
	if connected < 0 {
		return false, cgoError(cError, "unknown Bluetooth error")
	}

	return connected == 1, nil
}

func setDefaultAudioOutput(targetName string) (string, error) {
	cTargetName := C.CString(targetName)
	defer C.free(unsafe.Pointer(cTargetName))

	var cError *C.char
	cOutputName := C.FocusSetDefaultAudioOutputDevice(cTargetName, &cError)
	if cOutputName == nil {
		return "", cgoError(cError, "unknown audio routing error")
	}
	defer C.free(unsafe.Pointer(cOutputName))

	return C.GoString(cOutputName), nil
}

func isAudioOutputAvailable(targetName string) (bool, error) {
	cTargetName := C.CString(targetName)
	defer C.free(unsafe.Pointer(cTargetName))

	var cError *C.char
	available := C.FocusIsAudioOutputDeviceAvailable(cTargetName, &cError)
	if available < 0 {
		return false, cgoError(cError, "unknown audio output error")
	}

	return available == 1, nil
}

func cgoError(cError *C.char, fallback string) error {
	if cError == nil {
		return fmt.Errorf("%s", fallback)
	}
	defer C.free(unsafe.Pointer(cError))
	return fmt.Errorf("%s", C.GoString(cError))
}
