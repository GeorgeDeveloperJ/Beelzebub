package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
)

// Result captures the outcome of a command execution.
type Result struct {
	Passed     bool          `json:"passed"`
	ExitCode   int           `json:"exit_code"`
	Stdout     string        `json:"stdout"`
	Stderr     string        `json:"stderr"`
	Duration   time.Duration `json:"duration"`
	DurationMs int64         `json:"duration_ms"`
	Err        error         `json:"error,omitempty"`
}

// Runner executes shell commands and evaluates probe outcomes.
type Runner struct{}

// New creates a new command runner instance.
func New() *Runner {
	return &Runner{}
}

// ExecuteCommand executes a raw shell command string with a specified timeout.
func (r *Runner) ExecuteCommand(ctx context.Context, cmdStr string, timeout time.Duration) *Result {
	start := time.Now()
	res := &Result{}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", cmdStr)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	duration := time.Since(start)
	res.Duration = duration
	res.DurationMs = duration.Milliseconds()
	res.Stdout = strings.TrimSpace(stdoutBuf.String())
	res.Stderr = strings.TrimSpace(stderrBuf.String())

	if err != nil {
		res.Err = err
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		res.Passed = false
	} else {
		res.ExitCode = 0
		res.Passed = true
	}

	return res
}

// ExecuteProbe evaluates a sentinel's configured probe (shell exit code or numerical threshold).
func (r *Runner) ExecuteProbe(ctx context.Context, probe config.ProbeConfig, timeout time.Duration) *Result {
	res := r.ExecuteCommand(ctx, probe.Command, timeout)
	if !res.Passed {
		return res
	}

	// For threshold probes, evaluate command stdout as a numerical value
	if strings.ToLower(probe.Type) == "threshold" && probe.MinThreshold != nil {
		cleanOut := strings.TrimSpace(res.Stdout)
		// Strip possible units or quotes
		cleanOut = strings.Trim(cleanOut, "\"'` \t\r\n")

		val, err := strconv.ParseFloat(cleanOut, 64)
		if err != nil {
			res.Passed = false
			res.Err = fmt.Errorf("failed to parse threshold value %q as float: %w", cleanOut, err)
			return res
		}

		if val < *probe.MinThreshold {
			res.Passed = false
			res.Err = fmt.Errorf("measured value %.2f is below minimum threshold %.2f", val, *probe.MinThreshold)
			return res
		}
		res.Passed = true
	}

	return res
}

// ExecuteRemediation executes an automated remediation playbook command.
func (r *Runner) ExecuteRemediation(ctx context.Context, rem config.RemediationConfig, timeout time.Duration) *Result {
	return r.ExecuteCommand(ctx, rem.Command, timeout)
}
