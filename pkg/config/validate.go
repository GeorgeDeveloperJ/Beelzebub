package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var sentinelNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Valid notification providers.
var validProviders = map[string]bool{
	"ntfy":     true,
	"discord":  true,
	"telegram": true,
}

// Valid probe types.
var validProbeTypes = map[string]bool{
	"shell":     true,
	"threshold": true,
}

// Validate executes all schema and business invariant checks on the Config.
func (c *Config) Validate() error {
	var errs []error

	if strings.TrimSpace(c.Version) == "" {
		errs = append(errs, errors.New("config version is required"))
	}

	// Validate global configuration
	if c.Global.CheckIntervalDefault.Duration() > 0 && c.Global.CheckIntervalDefault.Duration() < time.Second {
		errs = append(errs, fmt.Errorf("global.check_interval_default must be at least 1s, got %v", c.Global.CheckIntervalDefault))
	}

	if err := c.validateNotifications(&c.Global.Notifications); err != nil {
		errs = append(errs, err)
	}

	// Validate sentinels
	if len(c.Sentinels) == 0 {
		errs = append(errs, errors.New("at least one sentinel must be configured under 'sentinels'"))
	}

	namesSeen := make(map[string]bool)
	for i, s := range c.Sentinels {
		if err := c.validateSentinel(i, &s, namesSeen); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *Config) validateNotifications(n *NotificationsConfig) error {
	if strings.TrimSpace(n.Endpoint) == "" {
		return nil
	}

	var errs []error
	provider := strings.ToLower(strings.TrimSpace(n.Provider))
	if !validProviders[provider] {
		errs = append(errs, fmt.Errorf("global.notifications.provider %q is invalid (must be 'ntfy', 'discord', or 'telegram')", n.Provider))
	}

	u, err := url.ParseRequestURI(n.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		errs = append(errs, fmt.Errorf("global.notifications.endpoint %q must be a valid http or https URL", n.Endpoint))
	}

	return errors.Join(errs...)
}

func (c *Config) validateSentinel(idx int, s *SentinelConfig, namesSeen map[string]bool) error {
	var errs []error
	prefix := fmt.Sprintf("sentinels[%d]", idx)
	name := strings.TrimSpace(s.Name)

	if name == "" {
		errs = append(errs, fmt.Errorf("%s: name is required", prefix))
	} else if !sentinelNameRegex.MatchString(name) {
		errs = append(errs, fmt.Errorf("%s: name %q must contain only alphanumeric characters, dashes, or underscores", prefix, name))
	} else if namesSeen[name] {
		errs = append(errs, fmt.Errorf("%s: duplicate sentinel name %q", prefix, name))
	} else {
		namesSeen[name] = true
		prefix = fmt.Sprintf("sentinel(%s)", name)
	}

	effectiveInterval := s.EffectiveInterval(c.Global.CheckIntervalDefault)
	if effectiveInterval < time.Second {
		errs = append(errs, fmt.Errorf("%s: interval must be at least 1s, got %v", prefix, effectiveInterval))
	}

	timeout := s.Timeout.Duration()
	if timeout <= 0 {
		errs = append(errs, fmt.Errorf("%s: timeout must be positive, got %v", prefix, timeout))
	} else if timeout >= effectiveInterval {
		errs = append(errs, fmt.Errorf("%s: timeout (%v) must be strictly less than interval (%v) to prevent overlap", prefix, timeout, effectiveInterval))
	}

	// Validate probe
	probeType := strings.ToLower(strings.TrimSpace(s.Probe.Type))
	if !validProbeTypes[probeType] {
		errs = append(errs, fmt.Errorf("%s: probe.type %q is invalid (must be 'shell' or 'threshold')", prefix, s.Probe.Type))
	}

	if strings.TrimSpace(s.Probe.Command) == "" {
		errs = append(errs, fmt.Errorf("%s: probe.command cannot be empty", prefix))
	}

	if probeType == "threshold" && s.Probe.MinThreshold == nil {
		errs = append(errs, fmt.Errorf("%s: probe.min_threshold is required when probe.type is 'threshold'", prefix))
	}

	// Validate remediation if present
	if s.Remediation != nil {
		if s.Remediation.MaxRetries < 1 {
			errs = append(errs, fmt.Errorf("%s: remediation.max_retries must be at least 1, got %d", prefix, s.Remediation.MaxRetries))
		}
		if s.Remediation.Backoff.Duration() < 0 {
			errs = append(errs, fmt.Errorf("%s: remediation.backoff cannot be negative, got %v", prefix, s.Remediation.Backoff))
		}
		if strings.TrimSpace(s.Remediation.Command) == "" {
			errs = append(errs, fmt.Errorf("%s: remediation.command cannot be empty when remediation is defined", prefix))
		}
	}

	return errors.Join(errs...)
}
