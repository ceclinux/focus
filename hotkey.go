package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	hotkeyLaunchAgentLabel = "com.local.focus.hotkey"
	skhdStartMarker        = "# >>> focus hotkey >>>"
	skhdEndMarker          = "# <<< focus hotkey <<<"
)

func installHotkeyLaunchAgent() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("global hotkey installation is currently implemented only on macOS")
	}

	executable, err := currentExecutable()
	if err != nil {
		return err
	}

	// Prefer skhd when available. This machine already runs skhd, and skhd is
	// much more reliable for user-level global shortcuts than a raw Carbon
	// hotkey listener running as a LaunchAgent.
	if _, err := exec.LookPath("skhd"); err == nil {
		_ = uninstallLaunchAgentOnly()
		return installSkhdHotkey(executable)
	}

	return installLaunchAgentHotkey(executable)
}

func uninstallHotkeyLaunchAgent() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("global hotkey installation is currently implemented only on macOS")
	}

	var errs []string
	if err := uninstallSkhdHotkey(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := uninstallLaunchAgentOnly(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func currentExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(executable)
}

func installSkhdHotkey(executable string) error {
	skhdrc, err := skhdrcPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(skhdrc), 0o755); err != nil {
		return err
	}

	logPath, err := focusHotkeyPlaybackLogPath()
	if err != nil {
		return err
	}

	content := ""
	if bytes, err := os.ReadFile(skhdrc); err == nil {
		content = string(bytes)
	} else if !os.IsNotExist(err) {
		return err
	}

	content = removeMarkedBlock(content, skhdStartMarker, skhdEndMarker)
	if strings.TrimSpace(content) != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	block := fmt.Sprintf(`%s
cmd + shift + ctrl - f : %s -toggle -noui >> %s 2>&1 &
%s
`, skhdStartMarker, shellQuote(executable), shellQuote(logPath), skhdEndMarker)
	content += block

	if err := os.WriteFile(skhdrc, []byte(content), 0o644); err != nil {
		return err
	}

	return reloadSkhd()
}

func uninstallSkhdHotkey() error {
	skhdrc, err := skhdrcPath()
	if err != nil {
		return err
	}

	bytes, err := os.ReadFile(skhdrc)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	content := removeMarkedBlock(string(bytes), skhdStartMarker, skhdEndMarker)
	return os.WriteFile(skhdrc, []byte(content), 0o644)
}

func skhdrcPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if value := strings.TrimSpace(os.Getenv("SKHD_CONFIG")); value != "" {
		return value, nil
	}
	return filepath.Join(home, ".config", "skhd", "skhdrc"), nil
}

func reloadSkhd() error {
	if err := exec.Command("pkill", "-USR1", "skhd").Run(); err == nil {
		return nil
	}
	if exec.Command("pgrep", "skhd").Run() == nil {
		return fmt.Errorf("skhd is running, but reload with pkill -USR1 skhd failed")
	}
	return exec.Command("skhd").Start()
}

func installLaunchAgentHotkey(executable string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return err
	}

	plistPath := hotkeyLaunchAgentPath()
	logPath := filepath.Join(home, "Library", "Logs", "focus-hotkey.log")
	errPath := filepath.Join(home, "Library", "Logs", "focus-hotkey.err.log")

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>-hotkey-daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, hotkeyLaunchAgentLabel, plistEscape(executable), plistEscape(logPath), plistEscape(errPath))

	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}

	_ = runLaunchctl("bootout", launchctlDomain(), plistPath)
	if err := runLaunchctl("bootstrap", launchctlDomain(), plistPath); err != nil {
		return err
	}
	return runLaunchctl("kickstart", "-k", launchctlDomain()+"/"+hotkeyLaunchAgentLabel)
}

func uninstallLaunchAgentOnly() error {
	plistPath := hotkeyLaunchAgentPath()
	_ = runLaunchctl("bootout", launchctlDomain(), plistPath)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func hotkeyLaunchAgentPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", hotkeyLaunchAgentLabel+".plist")
}

func focusHotkeyPlaybackLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "focus-hotkey-playback.log"), nil
}

func launchctlDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeMarkedBlock(content, start, end string) string {
	startIndex := strings.Index(content, start)
	if startIndex == -1 {
		return content
	}
	endIndex := strings.Index(content[startIndex:], end)
	if endIndex == -1 {
		return content
	}
	endIndex += startIndex + len(end)
	for endIndex < len(content) && (content[endIndex] == '\n' || content[endIndex] == '\r') {
		endIndex++
	}
	return content[:startIndex] + content[endIndex:]
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func plistEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	value = strings.ReplaceAll(value, "'", "&apos;")
	return value
}
