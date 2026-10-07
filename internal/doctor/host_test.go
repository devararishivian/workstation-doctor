package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectionHelper(_ *testing.T) {
	if os.Getenv("DOCTOR_INSPECTION_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "fail":
		fmt.Fprint(os.Stdout, "usable but failed")
		os.Exit(2)
	case "large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 128))
		fmt.Fprint(os.Stderr, strings.Repeat("y", 128))
		os.Exit(0)
	}
	os.Exit(0)
}

func TestInspectionExitSemantics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	host, err := NewHost(Scope{}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.RunRead(t.Context(), Command{Executable: executable, Args: []string{"-test.run=TestInspectionHelper", "--", "fail"}, Env: map[string]string{"DOCTOR_INSPECTION_HELPER": "1"}})
	if err == nil || result.ExitCode != 2 || string(result.Stdout) != "usable but failed" {
		t.Fatalf("nonzero exit was accepted: %+v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := host.RunRead(ctx, Command{Executable: executable}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestInspectionLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	limits := DefaultLimits()
	limits.MaxCaptureBytes = 64
	host, err := NewHost(Scope{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.RunRead(t.Context(), Command{Executable: executable, Args: []string{"-test.run=TestInspectionHelper", "--", "large"}, Env: map[string]string{"DOCTOR_INSPECTION_HELPER": "1"}})
	if err == nil || !result.Truncated || len(result.Stdout)+len(result.Stderr) != 64 {
		t.Fatalf("capture was not bounded: %+v, %v", result, err)
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 65)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBounded(t.Context(), path, 64); err == nil {
		t.Fatal("oversized file accepted")
	}
	raw, err := ReadBounded(t.Context(), path, 65)
	if err != nil || len(raw) != 65 {
		t.Fatalf("file boundary: %d, %v", len(raw), err)
	}
	if _, err := ReadBounded(t.Context(), filepath.Dir(path), 65); err == nil {
		t.Fatal("directory accepted as file")
	}
}

func TestInspectionHTTPBound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, strings.Repeat("x", 65)) }))
	defer server.Close()
	limits := DefaultLimits()
	limits.MaxHTTPBytes = 64
	if _, err := fetchMetadata(t.Context(), server.Client(), server.URL, limits); err == nil {
		t.Fatal("oversized HTTP response accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fetchMetadata(ctx, server.Client(), server.URL, limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("HTTP cancellation: %v", err)
	}
	if _, err := fetchMetadata(t.Context(), server.Client(), "http://example.invalid", limits); err == nil {
		t.Fatal("insecure metadata URL accepted")
	}
}
