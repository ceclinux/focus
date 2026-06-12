# focus

`focus` toggles `kew` playback. If a `kew` process is already running, it pauses it if playing or resumes it if paused. If no `kew` process is running, it checks whether the audio output `rcs app2` is already available, routes audio to it, then starts `kew` playing `Chill Rain Jazz.mp3`.

While `kew` is running, `focus` watches the playback audio output. If `rcs app2` disconnects and the output disappears, `focus` pauses `kew` instead of killing it.

## Build

```sh
go build -o focus .
```

GitHub Actions builds release archives for Intel Macs (`darwin-amd64`) and Apple silicon Macs (`darwin-arm64`) whenever a `v*` tag is pushed. You can also run the **Release** workflow manually with a tag such as `v0.1.0`.

Optionally install it on your PATH:

```sh
go install .
```

## Usage

```sh
focus
```

## Global hotkey

Install the global hotkey `Ctrl+Shift+Command+F`:

```sh
focus -install-hotkey
```

If `skhd` is installed, this registers the binding in `~/.config/skhd/skhdrc` and reloads `skhd`. Otherwise it falls back to a user LaunchAgent. When the shortcut is pressed, it runs `focus -toggle -noui` in the background. Press once to start playback; press again to pause; press again to resume.

Uninstall it with:

```sh
focus -uninstall-hotkey
```

Override the device or default kew query:

```sh
focus -device "rcs app2" -query "Chill Rain Jazz.mp3"
focus "another kew search"
```

Environment overrides are also supported:

```sh
FOCUS_BT_DEVICE="rcs app2" FOCUS_KEW_QUERY="Chill Rain Jazz.mp3" focus
```

Bluetooth auto-connect, audio routing, and disconnect monitoring use macOS APIs, so they currently work on macOS only.
