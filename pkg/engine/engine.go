package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/notifier"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/runner"
	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/store"
)

// Engine orchestrates background sentinel health evaluation, remediation, and alerting.
type Engine struct {
	cfg      *config.Config
	store    *store.Store
	runner   *runner.Runner
	notifier *notifier.Notifier

	mu       sync.Mutex
	running  bool
	cancelFn context.CancelFunc
	wg       sync.WaitGroup
}

// New creates a new Engine instance.
func New(cfg *config.Config, s *store.Store) *Engine {
	return &Engine{
		cfg:      cfg,
		store:    s,
		runner:   runner.New(),
		notifier: notifier.New(cfg.Global.Notifications),
	}
}

// Start launches background goroutines for all configured sentinels.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("engine is already running")
	}

	runCtx, cancel := context.WithCancel(ctx)
	e.cancelFn = cancel
	e.running = true
	e.mu.Unlock()

	// Initialize sentinel states in store if not present
	for _, sc := range e.cfg.Sentinels {
		currState, err := e.store.GetSentinelState(sc.Name)
		if err == nil && currState == nil {
			_ = e.store.UpdateSentinelState(&store.SentinelState{
				SentinelName:        sc.Name,
				Status:              store.StatusPass,
				ConsecutiveFailures: 0,
				LastCheckedAt:       time.Now().UTC(),
			})
		}
	}

	for _, sc := range e.cfg.Sentinels {
		e.wg.Add(1)
		go e.sentinelLoop(runCtx, sc)
	}

	return nil
}

// Stop gracefully shuts down all sentinel background loops.
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.cancelFn()
	e.running = false
	e.mu.Unlock()

	e.wg.Wait()
}

func (e *Engine) sentinelLoop(ctx context.Context, sc config.SentinelConfig) {
	defer e.wg.Done()

	interval := sc.EffectiveInterval(e.cfg.Global.CheckIntervalDefault)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial immediate check
	e.EvaluateSentinel(ctx, sc)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if exhausted; if so, skip automatic rerun
			st, err := e.store.GetSentinelState(sc.Name)
			if err == nil && st != nil && st.Status == store.StatusExhausted {
				continue
			}
			e.EvaluateSentinel(ctx, sc)
		}
	}
}

// EvaluateSentinel runs the probe and executes remediation if check fails.
func (e *Engine) EvaluateSentinel(ctx context.Context, sc config.SentinelConfig) *store.SentinelState {
	timeout := sc.Timeout.Duration()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	res := e.runner.ExecuteProbe(ctx, sc.Probe, timeout)

	st, err := e.store.GetSentinelState(sc.Name)
	if err != nil || st == nil {
		st = &store.SentinelState{
			SentinelName: sc.Name,
			Status:       store.StatusPass,
		}
	}

	st.LastCheckedAt = time.Now().UTC()
	st.LastDurationMs = res.DurationMs
	st.LastOutput = res.Stdout
	if res.Stderr != "" {
		st.LastOutput = res.Stderr
	}

	if res.Passed {
		// Probe passed
		if st.Status == store.StatusRemediating || st.ConsecutiveFailures > 0 {
			// Resolved / Healed
			st.Status = store.StatusHealed
			st.ConsecutiveFailures = 0
			_ = e.store.RecordEvent(&store.Event{
				SentinelName: sc.Name,
				Timestamp:    time.Now().UTC(),
				Status:       store.StatusHealed,
				Stdout:       res.Stdout,
				Stderr:       res.Stderr,
				DurationMs:   res.DurationMs,
			})
			_ = e.notifier.Dispatch(ctx, "healed", sc.Name, fmt.Sprintf("Sentinel %s recovered and is now healthy.", sc.Name), res.Stdout)
		} else {
			st.Status = store.StatusPass
			_ = e.store.RecordEvent(&store.Event{
				SentinelName: sc.Name,
				Timestamp:    time.Now().UTC(),
				Status:       store.StatusPass,
				Stdout:       res.Stdout,
				Stderr:       res.Stderr,
				DurationMs:   res.DurationMs,
			})
		}
		_ = e.store.UpdateSentinelState(st)
		return st
	}

	// Probe failed
	st.ConsecutiveFailures++
	maxRetries := 1
	if sc.Remediation != nil && sc.Remediation.MaxRetries > 0 {
		maxRetries = sc.Remediation.MaxRetries
	}

	if st.ConsecutiveFailures > maxRetries {
		// Escalated to EXHAUSTED
		st.Status = store.StatusExhausted
		_ = e.store.RecordEvent(&store.Event{
			SentinelName: sc.Name,
			Timestamp:    time.Now().UTC(),
			Status:       store.StatusExhausted,
			Stdout:       res.Stdout,
			Stderr:       res.Stderr,
			DurationMs:   res.DurationMs,
		})
		msg := sc.OnExhausted
		if msg == "" {
			msg = fmt.Sprintf("Sentinel %s exhausted all %d remediation retries.", sc.Name, maxRetries)
		}
		_ = e.notifier.Dispatch(ctx, "exhausted", sc.Name, msg, fmt.Sprintf("Last error: %s (exit %d)", res.Stderr, res.ExitCode))
		_ = e.store.UpdateSentinelState(st)
		return st
	}

	// In Remediating phase
	st.Status = store.StatusRemediating
	_ = e.store.RecordEvent(&store.Event{
		SentinelName: sc.Name,
		Timestamp:    time.Now().UTC(),
		Status:       store.StatusRemediating,
		Stdout:       res.Stdout,
		Stderr:       res.Stderr,
		DurationMs:   res.DurationMs,
	})
	_ = e.store.UpdateSentinelState(st)

	// Execute remediation playbook if defined
	if sc.Remediation != nil && sc.Remediation.Command != "" {
		remTimeout := 30 * time.Second
		remRes := e.runner.ExecuteRemediation(ctx, *sc.Remediation, remTimeout)
		log.Printf("[%s] Executed remediation (attempt %d/%d): exit %d, took %v",
			sc.Name, st.ConsecutiveFailures, maxRetries, remRes.ExitCode, remRes.Duration)

		if sc.Remediation.Backoff.Duration() > 0 {
			time.Sleep(sc.Remediation.Backoff.Duration())
		}
	}

	return st
}

// ForceCheck triggers an immediate manual evaluation of a sentinel by name.
func (e *Engine) ForceCheck(ctx context.Context, name string) (*store.SentinelState, error) {
	for _, sc := range e.cfg.Sentinels {
		if sc.Name == name {
			return e.EvaluateSentinel(ctx, sc), nil
		}
	}
	return nil, fmt.Errorf("sentinel %q not found in configuration", name)
}

// ForceRemediation triggers an immediate manual execution of a sentinel's remediation playbook.
func (e *Engine) ForceRemediation(ctx context.Context, name string) (*runner.Result, error) {
	for _, sc := range e.cfg.Sentinels {
		if sc.Name == name {
			if sc.Remediation == nil || sc.Remediation.Command == "" {
				return nil, fmt.Errorf("sentinel %q has no remediation playbook configured", name)
			}
			res := e.runner.ExecuteRemediation(ctx, *sc.Remediation, 30*time.Second)
			_ = e.store.RecordEvent(&store.Event{
				SentinelName: name,
				Timestamp:    time.Now().UTC(),
				Status:       store.StatusRemediating,
				Stdout:       res.Stdout,
				Stderr:       res.Stderr,
				DurationMs:   res.DurationMs,
			})
			return res, nil
		}
	}
	return nil, fmt.Errorf("sentinel %q not found in configuration", name)
}

// ResetSentinel resets the consecutive failure counters and exhausted state.
func (e *Engine) ResetSentinel(name string) error {
	return e.store.ResetSentinelState(name)
}
