package store

import (
	"testing"
	"time"
)

func TestStore_RecordAndRetrieveEvents(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init in-memory store: %v", err)
	}
	defer s.Close()

	e1 := &Event{
		SentinelName: "canary-check",
		Timestamp:    time.Now().UTC(),
		Status:       StatusPass,
		Stdout:       "file exists",
		Stderr:       "",
		DurationMs:   12,
	}
	if err := s.RecordEvent(e1); err != nil {
		t.Fatalf("RecordEvent failed: %v", err)
	}
	if e1.ID == 0 {
		t.Fatalf("expected non-zero event ID")
	}

	e2 := &Event{
		SentinelName: "speedtest",
		Timestamp:    time.Now().UTC(),
		Status:       StatusRemediating,
		Stdout:       "42 Mbps",
		Stderr:       "below threshold",
		DurationMs:   1250,
	}
	if err := s.RecordEvent(e2); err != nil {
		t.Fatalf("RecordEvent 2 failed: %v", err)
	}

	events, err := s.GetRecentEvents(10)
	if err != nil {
		t.Fatalf("GetRecentEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Ordered descending by ID: newest first
	if events[0].SentinelName != "speedtest" {
		t.Errorf("expected newest event to be speedtest, got %q", events[0].SentinelName)
	}
}

func TestStore_SentinelStateLifecycle(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init in-memory store: %v", err)
	}
	defer s.Close()

	state := &SentinelState{
		SentinelName:        "media-mount",
		Status:              StatusRemediating,
		ConsecutiveFailures: 2,
		LastCheckedAt:       time.Now().UTC(),
		LastDurationMs:      15,
		LastOutput:          "mount failed",
	}

	if err := s.UpdateSentinelState(state); err != nil {
		t.Fatalf("UpdateSentinelState failed: %v", err)
	}

	fetched, err := s.GetSentinelState("media-mount")
	if err != nil || fetched == nil {
		t.Fatalf("GetSentinelState failed: %v", err)
	}
	if fetched.ConsecutiveFailures != 2 || fetched.Status != StatusRemediating {
		t.Errorf("unexpected state: %+v", fetched)
	}

	// Reset state
	if err := s.ResetSentinelState("media-mount"); err != nil {
		t.Fatalf("ResetSentinelState failed: %v", err)
	}

	resetState, err := s.GetSentinelState("media-mount")
	if err != nil || resetState == nil {
		t.Fatalf("GetSentinelState after reset failed: %v", err)
	}
	if resetState.ConsecutiveFailures != 0 || resetState.Status != StatusPass {
		t.Errorf("expected state to reset to 0/PASS, got: %+v", resetState)
	}
}
