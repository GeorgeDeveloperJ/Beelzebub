package engine

import (
	"context"
	"testing"
	"time"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/store"
)

func TestEngine_PassAndHealCycle(t *testing.T) {
	s, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer s.Close()

	cfg := &config.Config{
		Version: "1",
		Sentinels: []config.SentinelConfig{
			{
				Name:     "test-canary",
				Interval: config.Duration(10 * time.Second),
				Timeout:  config.Duration(2 * time.Second),
				Probe: config.ProbeConfig{
					Type:    "shell",
					Command: "echo 'ok'",
				},
			},
		},
	}

	eng := New(cfg, s)
	ctx := context.Background()

	// Initial evaluation passes
	st := eng.EvaluateSentinel(ctx, cfg.Sentinels[0])
	if st.Status != store.StatusPass {
		t.Errorf("expected status PASS, got %s", st.Status)
	}

	events, _ := s.GetRecentEvents(5)
	if len(events) != 1 || events[0].Status != store.StatusPass {
		t.Errorf("expected 1 PASS event, got %+v", events)
	}
}

func TestEngine_FailureAndExhaustion(t *testing.T) {
	s, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer s.Close()

	cfg := &config.Config{
		Version: "1",
		Sentinels: []config.SentinelConfig{
			{
				Name:     "failing-canary",
				Interval: config.Duration(10 * time.Second),
				Timeout:  config.Duration(2 * time.Second),
				Probe: config.ProbeConfig{
					Type:    "shell",
					Command: "exit 1",
				},
				Remediation: &config.RemediationConfig{
					MaxRetries: 2,
					Backoff:    config.Duration(10 * time.Millisecond),
					Command:    "echo 'remediating'",
				},
				OnExhausted: "Canary completely exhausted",
			},
		},
	}

	eng := New(cfg, s)
	ctx := context.Background()

	// Attempt 1: REMEDIATING
	st1 := eng.EvaluateSentinel(ctx, cfg.Sentinels[0])
	if st1.Status != store.StatusRemediating || st1.ConsecutiveFailures != 1 {
		t.Errorf("expected REMEDIATING (1 failure), got %+v", st1)
	}

	// Attempt 2: REMEDIATING
	st2 := eng.EvaluateSentinel(ctx, cfg.Sentinels[0])
	if st2.Status != store.StatusRemediating || st2.ConsecutiveFailures != 2 {
		t.Errorf("expected REMEDIATING (2 failures), got %+v", st2)
	}

	// Attempt 3: EXHAUSTED (> maxRetries 2)
	st3 := eng.EvaluateSentinel(ctx, cfg.Sentinels[0])
	if st3.Status != store.StatusExhausted {
		t.Errorf("expected EXHAUSTED, got %+v", st3)
	}

	// ResetSentinel clears state
	if err := eng.ResetSentinel("failing-canary"); err != nil {
		t.Fatalf("ResetSentinel failed: %v", err)
	}
	resetSt, _ := s.GetSentinelState("failing-canary")
	if resetSt.ConsecutiveFailures != 0 || resetSt.Status != store.StatusPass {
		t.Errorf("expected reset to 0/PASS, got %+v", resetSt)
	}
}
