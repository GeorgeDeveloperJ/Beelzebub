package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a custom wrapper around time.Duration that supports YAML string unmarshaling (e.g. "30s", "15m").
type Duration time.Duration

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String implements fmt.Stringer.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// UnmarshalYAML parses duration strings such as "10s", "5m", or "1h".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration format %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML serializes the Duration to standard string representation.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// Config represents the complete declarative configuration in sentinels.yaml.
type Config struct {
	Version   string           `yaml:"version"`
	Global    GlobalConfig     `yaml:"global"`
	Sentinels []SentinelConfig `yaml:"sentinels"`
}

// GlobalConfig contains fleet-level fallback defaults and notification settings.
type GlobalConfig struct {
	CheckIntervalDefault Duration            `yaml:"check_interval_default"`
	Notifications        NotificationsConfig `yaml:"notifications"`
}

// NotificationsConfig specifies webhook alert dispatching settings.
type NotificationsConfig struct {
	Provider string   `yaml:"provider"` // "ntfy", "discord", "telegram"
	Endpoint string   `yaml:"endpoint"`
	AlertOn  []string `yaml:"alert_on"`
}

// SentinelConfig represents a single autonomous watchdog probe and its remediation rule.
type SentinelConfig struct {
	Name        string             `yaml:"name"`
	Interval    Duration           `yaml:"interval"`
	Timeout     Duration           `yaml:"timeout"`
	Probe       ProbeConfig        `yaml:"probe"`
	Remediation *RemediationConfig `yaml:"remediation,omitempty"`
	OnExhausted string             `yaml:"on_exhausted"`
}

// EffectiveInterval returns Sentinel interval or falls back to global default.
func (s *SentinelConfig) EffectiveInterval(defaultInterval Duration) time.Duration {
	if s.Interval.Duration() > 0 {
		return s.Interval.Duration()
	}
	if defaultInterval.Duration() > 0 {
		return defaultInterval.Duration()
	}
	return 60 * time.Second
}

// ProbeConfig defines the health check command and type.
type ProbeConfig struct {
	Type         string   `yaml:"type"` // "shell" or "threshold"
	Command      string   `yaml:"command"`
	MinThreshold *float64 `yaml:"min_threshold,omitempty"`
}

// RemediationConfig defines the automated remediation parameters.
type RemediationConfig struct {
	MaxRetries int      `yaml:"max_retries"`
	Backoff    Duration `yaml:"backoff"`
	Command    string   `yaml:"command"`
}

// LoadFromFile reads and parses a YAML configuration file from disk.
func LoadFromFile(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file %q: %w", filePath, err)
	}
	return ParseAndValidate(data)
}

// ParseYAML deserializes raw YAML bytes into a Config struct without validation.
func ParseYAML(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("YAML syntax error: %w", err)
	}
	return &cfg, nil
}

// ParseAndValidate deserializes and validates a Config struct.
func ParseAndValidate(data []byte) (*Config, error) {
	cfg, err := ParseYAML(data)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
