package runner

import (
	"context"
	"testing"
	"time"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
)

func TestRunner_ShellProbe(t *testing.T) {
	r := New()
	ctx := context.Background()

	// 1. Success probe
	passProbe := config.ProbeConfig{
		Type:    "shell",
		Command: "echo 'hello world'",
	}
	res := r.ExecuteProbe(ctx, passProbe, 2*time.Second)
	if !res.Passed {
		t.Fatalf("expected probe to pass, got err: %v", res.Err)
	}
	if res.Stdout != "hello world" {
		t.Errorf("expected stdout 'hello world', got %q", res.Stdout)
	}

	// 2. Failure probe
	failProbe := config.ProbeConfig{
		Type:    "shell",
		Command: "exit 1",
	}
	resFail := r.ExecuteProbe(ctx, failProbe, 2*time.Second)
	if resFail.Passed {
		t.Fatalf("expected probe to fail, but it passed")
	}
	if resFail.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", resFail.ExitCode)
	}
}

func TestRunner_ThresholdProbe(t *testing.T) {
	r := New()
	ctx := context.Background()

	minVal := 50.0
	// 1. Value above threshold: PASS
	passProbe := config.ProbeConfig{
		Type:         "threshold",
		Command:      "echo '75.5'",
		MinThreshold: &minVal,
	}
	res := r.ExecuteProbe(ctx, passProbe, 2*time.Second)
	if !res.Passed {
		t.Fatalf("expected 75.5 >= 50.0 to pass, got err: %v", res.Err)
	}

	// 2. Value below threshold: FAIL
	failProbe := config.ProbeConfig{
		Type:         "threshold",
		Command:      "echo '30.2'",
		MinThreshold: &minVal,
	}
	resFail := r.ExecuteProbe(ctx, failProbe, 2*time.Second)
	if resFail.Passed {
		t.Fatalf("expected 30.2 < 50.0 to fail, but it passed")
	}

	// 3. Corrupt/non-float output: FAIL
	badProbe := config.ProbeConfig{
		Type:         "threshold",
		Command:      "echo 'not-a-number'",
		MinThreshold: &minVal,
	}
	resBad := r.ExecuteProbe(ctx, badProbe, 2*time.Second)
	if resBad.Passed {
		t.Fatalf("expected corrupt non-float to fail, but it passed")
	}
}

func TestRunner_TimeoutKillsCommand(t *testing.T) {
	r := New()
	ctx := context.Background()

	slowProbe := config.ProbeConfig{
		Type:    "shell",
		Command: "sleep 2",
	}
	res := r.ExecuteProbe(ctx, slowProbe, 100*time.Millisecond)
	if res.Passed {
		t.Fatalf("expected command to be killed by timeout")
	}
	if res.Duration >= 1*time.Second {
		t.Errorf("timeout should have terminated command quickly, took %v", res.Duration)
	}
}
