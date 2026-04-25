package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProfileDir returns the directory where the dedicated Chrome profile lives.
// Cross-platform paths:
//   - Linux:   $XDG_CONFIG_HOME/deep-proxy/chrome-profile (or ~/.config/...)
//   - macOS:   ~/Library/Application Support/deep-proxy/chrome-profile
//   - Windows: %AppData%\deep-proxy\chrome-profile
func ProfileDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "deep-proxy", "chrome-profile"), nil
}

// EnsureProfileDir creates the profile directory if it doesn't exist.
func EnsureProfileDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

// UnlockProfile removes Chrome's singleton lock files from a previous unclean
// shutdown. Best-effort — errors are intentionally ignored.
//
// When Chrome crashes or is force-killed, these files remain and prevent a new
// instance from starting with the same profile. They are safe to delete when no
// Chrome process is currently using the profile.
func UnlockProfile(path string) {
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		_ = os.Remove(filepath.Join(path, name))
	}
}

// ProfileExists reports whether the profile directory contains a Chrome
// installation marker (the "Default" subdirectory created on first login).
func ProfileExists(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "Default")); err == nil {
		return true
	}
	return false
}
