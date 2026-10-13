package notifier

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GeorgeDeveloperJ/Beelzebub/pkg/config"
)

func TestNotifier_DispatchNtfy(t *testing.T) {
	receivedTitle := ""
	receivedBody := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTitle = r.Header.Get("Title")
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.NotificationsConfig{
		Provider: "ntfy",
		Endpoint: server.URL,
		AlertOn:  []string{"healed", "exhausted"},
	}
	n := New(cfg)
	ctx := context.Background()

	// 1. Alert that is suppressed
	err := n.Dispatch(ctx, "pass", "canary", "all good", "")
	if err != nil {
		t.Fatalf("unexpected error on suppressed alert: %v", err)
	}
	if receivedBody != "" {
		t.Fatalf("expected no dispatch for suppressed event 'pass'")
	}

	// 2. Alert that should dispatch
	err = n.Dispatch(ctx, "exhausted", "canary", "Failed to mount /mnt/media", "Exit code 1")
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	if receivedTitle != "Beelzebub: [EXHAUSTED] canary" {
		t.Errorf("unexpected title: %q", receivedTitle)
	}
	if receivedBody == "" {
		t.Errorf("expected non-empty body")
	}
}

func TestNotifier_DispatchDiscord(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.NotificationsConfig{
		Provider: "discord",
		Endpoint: server.URL,
		AlertOn:  []string{"healed"},
	}
	n := New(cfg)
	ctx := context.Background()

	err := n.Dispatch(ctx, "healed", "media-mount", "Remount succeeded", "")
	if err != nil {
		t.Fatalf("discord dispatch error: %v", err)
	}
	if !called {
		t.Errorf("expected server to be called")
	}
}
