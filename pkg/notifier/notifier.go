package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
)

// Notifier handles HTTP webhook dispatching to ntfy, discord, and telegram.
type Notifier struct {
	cfg        config.NotificationsConfig
	httpClient *http.Client
}

// New creates a new Notifier.
func New(cfg config.NotificationsConfig) *Notifier {
	return &Notifier{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ShouldAlert checks if the event type (e.g. "healed", "exhausted") is configured for dispatch.
func (n *Notifier) ShouldAlert(eventType string) bool {
	if strings.TrimSpace(n.cfg.Endpoint) == "" {
		return false
	}
	if len(n.cfg.AlertOn) == 0 {
		return true // Default: send all alerts if not constrained
	}
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	for _, a := range n.cfg.AlertOn {
		if strings.ToLower(strings.TrimSpace(a)) == eventType {
			return true
		}
	}
	return false
}

// Dispatch sends a webhook notification if enabled for the event type.
func (n *Notifier) Dispatch(ctx context.Context, eventType, sentinelName, summary, details string) error {
	if !n.ShouldAlert(eventType) {
		return nil
	}

	provider := strings.ToLower(strings.TrimSpace(n.cfg.Provider))
	endpoint := strings.TrimSpace(n.cfg.Endpoint)

	switch provider {
	case "ntfy":
		return n.sendNtfy(ctx, endpoint, eventType, sentinelName, summary, details)
	case "discord":
		return n.sendDiscord(ctx, endpoint, eventType, sentinelName, summary, details)
	case "telegram":
		return n.sendTelegram(ctx, endpoint, eventType, sentinelName, summary, details)
	default:
		return n.sendGeneric(ctx, endpoint, eventType, sentinelName, summary, details)
	}
}

func (n *Notifier) sendNtfy(ctx context.Context, endpoint, eventType, sentinelName, summary, details string) error {
	body := summary
	if details != "" {
		body = fmt.Sprintf("%s\n\n%s", summary, details)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}

	priority := "3"
	tags := "gear"
	if strings.ToLower(eventType) == "exhausted" {
		priority = "5"
		tags = "rotating_light,skull"
	} else if strings.ToLower(eventType) == "healed" {
		priority = "3"
		tags = "white_check_mark,sparkles"
	}

	req.Header.Set("Title", fmt.Sprintf("Beelzebub: [%s] %s", strings.ToUpper(eventType), sentinelName))
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy dispatch error: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendDiscord(ctx context.Context, endpoint, eventType, sentinelName, summary, details string) error {
	color := 3066993 // Green (healed)
	if strings.ToLower(eventType) == "exhausted" {
		color = 15158332 // Red (exhausted)
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       fmt.Sprintf("Beelzebub: [%s] %s", strings.ToUpper(eventType), sentinelName),
				"description": summary,
				"color":       color,
				"fields": []map[string]string{
					{"name": "Details", "value": details},
				},
				"footer": map[string]string{
					"text": "Homelab SRE Sentinel",
				},
			},
		},
	}

	return n.postJSON(ctx, endpoint, payload)
}

func (n *Notifier) sendTelegram(ctx context.Context, endpoint, eventType, sentinelName, summary, details string) error {
	text := fmt.Sprintf("🛡️ *Beelzebub Sentinel Alert*\n\n*Status:* %s\n*Sentinel:* `%s`\n*Summary:* %s",
		strings.ToUpper(eventType), sentinelName, summary)
	if details != "" {
		text += fmt.Sprintf("\n*Details:* `%s`", details)
	}

	payload := map[string]interface{}{
		"text":       text,
		"parse_mode": "Markdown",
	}

	return n.postJSON(ctx, endpoint, payload)
}

func (n *Notifier) sendGeneric(ctx context.Context, endpoint, eventType, sentinelName, summary, details string) error {
	payload := map[string]interface{}{
		"event_type":    eventType,
		"sentinel_name": sentinelName,
		"summary":       summary,
		"details":       details,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
	return n.postJSON(ctx, endpoint, payload)
}

func (n *Notifier) postJSON(ctx context.Context, endpoint string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post error: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
