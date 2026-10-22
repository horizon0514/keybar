package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings are the app's and the command line's, in settings.json.
type Settings struct {
	// Reminders: a notification when keys are due soon or overdue, once a
	// day; RemindDays before.
	Remind     bool `json:"remind"`
	RemindDays int  `json:"remindDays,omitempty"`
	// GrantMinutes is how long "allow for a while" lasts.
	GrantMinutes int `json:"grantMinutes,omitempty"`
	// CLIApproval: `keys get` and `keys run` without a terminal (as an
	// agent's shell runs them) need approval, as MCP requests do.
	CLIApproval bool `json:"cliApproval"`
	// The app: its global shortcut; LastReminded the day (2006-01-02) it
	// last reminded.
	Shortcut     string `json:"shortcut,omitempty"`
	LastReminded string `json:"lastReminded,omitempty"`
}

// DefaultSettings are the settings before any change.
func DefaultSettings() Settings {
	return Settings{Remind: true, RemindDays: SoonDays, GrantMinutes: 15, CLIApproval: true}
}

// LoadSettings reads dir's settings.
func LoadSettings(dir string) Settings {
	s := DefaultSettings()
	if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.RemindDays <= 0 {
		s.RemindDays = SoonDays
	}
	if s.GrantMinutes <= 0 {
		s.GrantMinutes = 15
	}
	return s
}

// SaveSettings writes dir's settings.
func SaveSettings(dir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "settings.json"), append(b, '\n'))
}
