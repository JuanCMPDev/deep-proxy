package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials are the per-session DeepSeek headers captured from a real
// browser. The Cookie + HifLeim pair typically expires after ~30 minutes
// (Cloudflare's cf_clearance TTL); the Token (bearer JWT) lasts days but
// re-issues on each login.
type Credentials struct {
	Token   string `json:"token,omitempty"`
	Cookie  string `json:"cookie"`
	HifLeim string `json:"hif_leim"`
	SavedAt int64  `json:"saved_at_unix"`
}

// SavedAtTime returns SavedAt as a time.Time.
func (c *Credentials) SavedAtTime() time.Time {
	return time.Unix(c.SavedAt, 0)
}

// CredFile returns the path where credentials are persisted.
func CredFile() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(base, "deep-proxy", "credentials.json"), nil
}

// ReadCredentials loads credentials from disk. Returns (nil, nil) if the file
// does not exist — that is treated as "not yet logged in", not an error.
func ReadCredentials() (*Credentials, error) {
	path, err := CredFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}
	return &c, nil
}

// WriteCredentials persists credentials to disk with mode 0600 (owner-only).
func WriteCredentials(c *Credentials) error {
	if c == nil {
		return errors.New("nil credentials")
	}
	path, err := CredFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}
