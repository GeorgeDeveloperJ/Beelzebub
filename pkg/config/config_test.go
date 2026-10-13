package config

import (
	"strings"
	"testing"
	"time"
)

const validSpecYAML = `
version: "1"
global:
  check_interval_default: "60s"
  notifications:
    provider: "ntfy"
    endpoint: "https://ntfy.sh/your-topic-here"
    alert_on: ["healed", "exhausted"]

sentinels:
  - name: "media-storage-mount"
    interval: "30s"
    timeout: "5s"
    probe:
      type: "shell"
      command: "test -f /mnt/media/.canary"
    remediation:
      max_retries: 3
      backoff: "10s"
      command: "mount -a || systemctl restart autofs"
    on_exhausted: "Failed to remount /mnt/media after 3 attempts."

  - name: "wan-speedtest-gateway"
    interval: "30m"
    timeout: "60s"
    probe:
      type: "threshold"
      command: "speedtest --format=json | jq '.download.bandwidth / 125000'"
      min_threshold: 50.0
    remediation:
      max_retries: 2
      backoff: "15s"
      command: "systemctl restart wg-quick@wg0"
    on_exhausted: "WAN speed remained degraded below 50 Mbps."
`

func TestParseAndValidate_ValidSpec(t *testing.T) {
	cfg, err := ParseAndValidate([]byte(validSpecYAML))
	if err != nil {
		t.Fatalf("expected valid spec to pass, got err: %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("expected version '1', got %q", cfg.Version)
	}

	if len(cfg.Sentinels) != 2 {
		t.Fatalf("expected 2 sentinels, got %d", len(cfg.Sentinels))
	}

	s1 := cfg.Sentinels[0]
	if s1.Name != "media-storage-mount" {
		t.Errorf("expected name 'media-storage-mount', got %q", s1.Name)
	}
	if s1.Interval.Duration() != 30*time.Second {
		t.Errorf("expected 30s interval, got %v", s1.Interval)
	}
	if s1.Timeout.Duration() != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", s1.Timeout)
	}
	if s1.Remediation == nil || s1.Remediation.MaxRetries != 3 {
		t.Errorf("expected max_retries 3, got %+v", s1.Remediation)
	}

	s2 := cfg.Sentinels[1]
	if s2.Probe.Type != "threshold" {
		t.Errorf("expected probe type 'threshold', got %q", s2.Probe.Type)
	}
	if s2.Probe.MinThreshold == nil || *s2.Probe.MinThreshold != 50.0 {
		t.Errorf("expected min_threshold 50.0, got %v", s2.Probe.MinThreshold)
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		errContains string
	}{
		{
			name: "missing version",
			yaml: `
sentinels:
  - name: "check1"
    interval: "10s"
    timeout: "2s"
    probe:
      type: "shell"
      command: "true"
`,
			errContains: "config version is required",
		},
		{
			name: "no sentinels",
			yaml: `
version: "1"
sentinels: []
`,
			errContains: "at least one sentinel must be configured",
		},
		{
			name: "timeout greater than interval",
			yaml: `
version: "1"
sentinels:
  - name: "slow-check"
    interval: "5s"
    timeout: "10s"
    probe:
      type: "shell"
      command: "sleep 1"
`,
			errContains: "must be strictly less than interval",
		},
		{
			name: "timeout equal to interval",
			yaml: `
version: "1"
sentinels:
  - name: "equal-check"
    interval: "5s"
    timeout: "5s"
    probe:
      type: "shell"
      command: "sleep 1"
`,
			errContains: "must be strictly less than interval",
		},
		{
			name: "duplicate sentinel name",
			yaml: `
version: "1"
sentinels:
  - name: "dup"
    interval: "10s"
    timeout: "2s"
    probe:
      type: "shell"
      command: "true"
  - name: "dup"
    interval: "10s"
    timeout: "2s"
    probe:
      type: "shell"
      command: "true"
`,
			errContains: "duplicate sentinel name \"dup\"",
		},
		{
			name: "threshold probe missing min_threshold",
			yaml: `
version: "1"
sentinels:
  - name: "bandwidth"
    interval: "10s"
    timeout: "2s"
    probe:
      type: "threshold"
      command: "echo 10"
`,
			errContains: "probe.min_threshold is required",
		},
		{
			name: "empty probe command",
			yaml: `
version: "1"
sentinels:
  - name: "blank-cmd"
    interval: "10s"
    timeout: "2s"
    probe:
      type: "shell"
      command: "   "
`,
			errContains: "probe.command cannot be empty",
		},
		{
			name: "invalid notification provider",
			yaml: `
version: "1"
global:
  notifications:
    provider: "slack"
    endpoint: "https://example.com"
sentinels:
  - name: "check1"
    interval: "10s"
    timeout: "2s"
    probe:
      type: "shell"
      command: "true"
`,
			errContains: "notifications.provider \"slack\" is invalid",
		},
		{
			name: "invalid duration syntax",
			yaml: `
version: "1"
sentinels:
  - name: "check1"
    interval: "not-a-duration"
    timeout: "2s"
    probe:
      type: "shell"
      command: "true"
`,
			errContains: "invalid duration format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAndValidate([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("expected error containing %q, got %q", tt.errContains, err.Error())
			}
		})
	}
}

func TestFallbackGlobalInterval(t *testing.T) {
	yamlData := `
version: "1"
global:
  check_interval_default: "45s"
sentinels:
  - name: "inherited-check"
    timeout: "5s"
    probe:
      type: "shell"
      command: "true"
`
	cfg, err := ParseAndValidate([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := cfg.Sentinels[0]
	effective := s.EffectiveInterval(cfg.Global.CheckIntervalDefault)
	if effective != 45*time.Second {
		t.Errorf("expected effective interval to be 45s, got %v", effective)
	}
}
