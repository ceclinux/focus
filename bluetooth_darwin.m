#import <Carbon/Carbon.h>
#import <CoreAudio/CoreAudio.h>
#import <Foundation/Foundation.h>
#import <IOBluetooth/IOBluetooth.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#ifndef kAudioObjectPropertyElementMain
#define kAudioObjectPropertyElementMain kAudioObjectPropertyElementMaster
#endif

extern void FocusHotkeyPressed(void);
static void setError(char **errorMessage, NSString *message);

static OSStatus focusHotkeyHandler(EventHandlerCallRef nextHandler, EventRef event, void *userData) {
    FocusHotkeyPressed();
    return noErr;
}

int FocusRunHotkeyLoop(char **errorMessage) {
    @autoreleasepool {
        if (errorMessage != NULL) {
            *errorMessage = NULL;
        }

        EventTypeSpec eventType;
        eventType.eventClass = kEventClassKeyboard;
        eventType.eventKind = kEventHotKeyPressed;

        OSStatus status = InstallApplicationEventHandler(&focusHotkeyHandler, 1, &eventType, NULL, NULL);
        if (status != noErr) {
            setError(errorMessage, [NSString stringWithFormat:@"InstallApplicationEventHandler failed with OSStatus 0x%08x", (unsigned int)status]);
            return (int)status;
        }

        EventHotKeyID hotKeyID;
        hotKeyID.signature = 'focs';
        hotKeyID.id = 1;

        EventHotKeyRef hotKeyRef = NULL;
        status = RegisterEventHotKey(kVK_ANSI_F, cmdKey | shiftKey | controlKey, hotKeyID, GetApplicationEventTarget(), 0, &hotKeyRef);
        if (status != noErr) {
            setError(errorMessage, [NSString stringWithFormat:@"RegisterEventHotKey for Ctrl+Shift+Command+F failed with OSStatus 0x%08x", (unsigned int)status]);
            return (int)status;
        }

        while (true) {
            EventRef event = NULL;
            status = ReceiveNextEvent(0, NULL, kEventDurationForever, true, &event);
            if (status == noErr && event != NULL) {
                SendEventToEventTarget(event, GetEventDispatcherTarget());
                ReleaseEvent(event);
            } else if (status != eventLoopTimedOutErr) {
                setError(errorMessage, [NSString stringWithFormat:@"hotkey event loop failed with OSStatus 0x%08x", (unsigned int)status]);
                return (int)status;
            }
        }

        return 0;
    }
}

static char *copyCString(NSString *value) {
    if (value == nil) {
        value = @"";
    }
    const char *utf8 = [value UTF8String];
    if (utf8 == NULL) {
        utf8 = "";
    }
    char *copy = (char *)malloc(strlen(utf8) + 1);
    if (copy != NULL) {
        strcpy(copy, utf8);
    }
    return copy;
}

static void setError(char **errorMessage, NSString *message) {
    if (errorMessage != NULL) {
        *errorMessage = copyCString(message);
    }
}

static NSString *targetString(const char *targetName) {
    NSString *target = [NSString stringWithUTF8String:targetName == NULL ? "" : targetName];
    return [target stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]];
}

static NSString *bluetoothDeviceName(IOBluetoothDevice *device) {
    NSString *name = [device name];
    if (name == nil || [name length] == 0) {
        name = [device nameOrAddress];
    }
    return name;
}

static BOOL nameMatches(NSString *name, NSString *target) {
    if (name == nil || [name length] == 0) {
        return NO;
    }
    return [name caseInsensitiveCompare:target] == NSOrderedSame;
}

static BOOL nameContains(NSString *name, NSString *target) {
    if (name == nil || [name length] == 0) {
        return NO;
    }
    return [name rangeOfString:target options:NSCaseInsensitiveSearch].location != NSNotFound;
}

static IOBluetoothDevice *findBluetoothDevice(NSString *target, NSMutableArray<NSString *> *availableNames) {
    NSArray *pairedDevices = [IOBluetoothDevice pairedDevices];
    IOBluetoothDevice *matchedDevice = nil;

    for (IOBluetoothDevice *device in pairedDevices) {
        NSString *name = bluetoothDeviceName(device);
        if (name != nil && [name length] > 0) {
            if (availableNames != nil) {
                [availableNames addObject:name];
            }
            if (nameMatches(name, target)) {
                matchedDevice = device;
                break;
            }
        }
    }

    if (matchedDevice == nil) {
        for (IOBluetoothDevice *device in pairedDevices) {
            NSString *name = bluetoothDeviceName(device);
            if (nameContains(name, target)) {
                matchedDevice = device;
                break;
            }
        }
    }

    return matchedDevice;
}

static NSString *audioObjectString(AudioObjectID objectID, AudioObjectPropertySelector selector) {
    CFStringRef value = NULL;
    UInt32 dataSize = sizeof(value);
    AudioObjectPropertyAddress address = {
        selector,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };

    OSStatus status = AudioObjectGetPropertyData(objectID, &address, 0, NULL, &dataSize, &value);
    if (status != noErr || value == NULL) {
        return nil;
    }

    NSString *string = [NSString stringWithString:(__bridge NSString *)value];
    CFRelease(value);
    return string;
}

static BOOL audioDeviceHasOutput(AudioDeviceID deviceID) {
    UInt32 dataSize = 0;
    AudioObjectPropertyAddress address = {
        kAudioDevicePropertyStreams,
        kAudioDevicePropertyScopeOutput,
        kAudioObjectPropertyElementMain,
    };

    OSStatus status = AudioObjectGetPropertyDataSize(deviceID, &address, 0, NULL, &dataSize);
    return status == noErr && dataSize > 0;
}

static NSArray<NSNumber *> *allAudioDevices(void) {
    UInt32 dataSize = 0;
    AudioObjectPropertyAddress address = {
        kAudioHardwarePropertyDevices,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };

    OSStatus status = AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &address, 0, NULL, &dataSize);
    if (status != noErr || dataSize == 0) {
        return @[];
    }

    AudioDeviceID *deviceIDs = (AudioDeviceID *)malloc(dataSize);
    if (deviceIDs == NULL) {
        return @[];
    }

    status = AudioObjectGetPropertyData(kAudioObjectSystemObject, &address, 0, NULL, &dataSize, deviceIDs);
    if (status != noErr) {
        free(deviceIDs);
        return @[];
    }

    NSUInteger count = dataSize / sizeof(AudioDeviceID);
    NSMutableArray<NSNumber *> *devices = [NSMutableArray arrayWithCapacity:count];
    for (NSUInteger index = 0; index < count; index++) {
        [devices addObject:@(deviceIDs[index])];
    }

    free(deviceIDs);
    return devices;
}

static AudioDeviceID findAudioOutputDevice(NSString *target, NSString **matchedName, NSArray<NSString *> **availableOutputNames) {
    AudioDeviceID exactMatch = kAudioObjectUnknown;
    AudioDeviceID containsMatch = kAudioObjectUnknown;
    NSString *exactName = nil;
    NSString *containsName = nil;
    NSMutableArray<NSString *> *outputNames = [NSMutableArray array];

    for (NSNumber *deviceNumber in allAudioDevices()) {
        AudioDeviceID deviceID = [deviceNumber unsignedIntValue];
        if (!audioDeviceHasOutput(deviceID)) {
            continue;
        }

        NSString *name = audioObjectString(deviceID, kAudioObjectPropertyName);
        if (name == nil || [name length] == 0) {
            continue;
        }
        [outputNames addObject:name];

        if (exactMatch == kAudioObjectUnknown && nameMatches(name, target)) {
            exactMatch = deviceID;
            exactName = name;
        } else if (containsMatch == kAudioObjectUnknown && nameContains(name, target)) {
            containsMatch = deviceID;
            containsName = name;
        }
    }

    if (availableOutputNames != NULL) {
        *availableOutputNames = outputNames;
    }

    if (exactMatch != kAudioObjectUnknown) {
        if (matchedName != NULL) {
            *matchedName = exactName;
        }
        return exactMatch;
    }

    if (containsMatch != kAudioObjectUnknown) {
        if (matchedName != NULL) {
            *matchedName = containsName;
        }
        return containsMatch;
    }

    return kAudioObjectUnknown;
}

char *FocusConnectBluetoothDevice(const char *targetName, char **errorMessage) {
    @autoreleasepool {
        if (errorMessage != NULL) {
            *errorMessage = NULL;
        }

        NSString *target = targetString(targetName);
        if ([target length] == 0) {
            setError(errorMessage, @"Bluetooth device name is empty");
            return NULL;
        }

        NSMutableArray<NSString *> *availableNames = [NSMutableArray array];
        IOBluetoothDevice *matchedDevice = findBluetoothDevice(target, availableNames);
        if (matchedDevice == nil) {
            NSString *available = [availableNames count] == 0 ? @"none" : [availableNames componentsJoinedByString:@", "];
            setError(errorMessage, [NSString stringWithFormat:@"could not find a paired Bluetooth device named '%@' (paired devices: %@)", target, available]);
            return NULL;
        }

        NSString *matchedName = bluetoothDeviceName(matchedDevice);
        if (![matchedDevice isConnected]) {
            IOReturn result = [matchedDevice openConnection];
            if (result != kIOReturnSuccess && result != kIOReturnBusy && result != kIOReturnExclusiveAccess) {
                setError(errorMessage, [NSString stringWithFormat:@"openConnection failed for '%@' with IOReturn 0x%08x", matchedName, result]);
                return NULL;
            }
        }

        for (int attempt = 0; attempt < 40; attempt++) {
            if ([matchedDevice isConnected]) {
                return copyCString(matchedName);
            }
            usleep(250000);
        }

        setError(errorMessage, [NSString stringWithFormat:@"timed out waiting for '%@' to connect", matchedName]);
        return NULL;
    }
}

int FocusIsBluetoothDeviceConnected(const char *targetName, char **errorMessage) {
    @autoreleasepool {
        if (errorMessage != NULL) {
            *errorMessage = NULL;
        }

        NSString *target = targetString(targetName);
        if ([target length] == 0) {
            setError(errorMessage, @"Bluetooth device name is empty");
            return -1;
        }

        IOBluetoothDevice *matchedDevice = findBluetoothDevice(target, nil);
        if (matchedDevice == nil) {
            setError(errorMessage, [NSString stringWithFormat:@"could not find paired Bluetooth device '%@'", target]);
            return -1;
        }

        return [matchedDevice isConnected] ? 1 : 0;
    }
}

int FocusIsAudioOutputDeviceAvailable(const char *targetName, char **errorMessage) {
    @autoreleasepool {
        if (errorMessage != NULL) {
            *errorMessage = NULL;
        }

        NSString *target = targetString(targetName);
        if ([target length] == 0) {
            setError(errorMessage, @"audio output device name is empty");
            return -1;
        }

        NSString *matchedName = nil;
        AudioDeviceID outputDevice = findAudioOutputDevice(target, &matchedName, NULL);
        return outputDevice == kAudioObjectUnknown ? 0 : 1;
    }
}

char *FocusSetDefaultAudioOutputDevice(const char *targetName, char **errorMessage) {
    @autoreleasepool {
        if (errorMessage != NULL) {
            *errorMessage = NULL;
        }

        NSString *target = targetString(targetName);
        if ([target length] == 0) {
            setError(errorMessage, @"audio output device name is empty");
            return NULL;
        }

        NSString *matchedName = nil;
        NSArray<NSString *> *availableOutputNames = nil;
        AudioDeviceID outputDevice = findAudioOutputDevice(target, &matchedName, &availableOutputNames);
        if (outputDevice == kAudioObjectUnknown) {
            NSString *available = [availableOutputNames count] == 0 ? @"none" : [availableOutputNames componentsJoinedByString:@", "];
            setError(errorMessage, [NSString stringWithFormat:@"could not find an audio output named '%@' (outputs: %@)", target, available]);
            return NULL;
        }

        AudioObjectPropertyAddress defaultOutputAddress = {
            kAudioHardwarePropertyDefaultOutputDevice,
            kAudioObjectPropertyScopeGlobal,
            kAudioObjectPropertyElementMain,
        };
        OSStatus status = AudioObjectSetPropertyData(kAudioObjectSystemObject, &defaultOutputAddress, 0, NULL, sizeof(outputDevice), &outputDevice);
        if (status != noErr) {
            setError(errorMessage, [NSString stringWithFormat:@"failed to set default audio output to '%@' with OSStatus 0x%08x", matchedName, (unsigned int)status]);
            return NULL;
        }

        AudioObjectPropertyAddress systemOutputAddress = {
            kAudioHardwarePropertyDefaultSystemOutputDevice,
            kAudioObjectPropertyScopeGlobal,
            kAudioObjectPropertyElementMain,
        };
        AudioObjectSetPropertyData(kAudioObjectSystemObject, &systemOutputAddress, 0, NULL, sizeof(outputDevice), &outputDevice);

        return copyCString(matchedName);
    }
}
