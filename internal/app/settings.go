package app

// Settings are the user's app preferences.
type Settings struct {
	SyncEnabled     bool `json:"syncEnabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
	StartAtLogin    bool `json:"startAtLogin"`
}

// DefaultSettings: sync on, every 5 minutes (as the Python LaunchAgent did).
func DefaultSettings() Settings {
	return Settings{SyncEnabled: true, IntervalMinutes: 5}
}

// Normalise clamps the interval to 1-60 minutes.
func (s Settings) Normalise() Settings {
	if s.IntervalMinutes < 1 {
		s.IntervalMinutes = 1
	}
	if s.IntervalMinutes > 60 {
		s.IntervalMinutes = 60
	}
	return s
}
