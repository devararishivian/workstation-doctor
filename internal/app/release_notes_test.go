package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"workstation-doctor/internal/doctor"
)

func TestReleaseNotesBounds(t *testing.T) {
	// Server returns oversized body with control characters
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Output text with ANSI escape sequence and 3MB body
		_, _ = w.Write([]byte("\x1b[31mRed Alert\x1b[0m\n"))
		_, _ = w.Write([]byte(strings.Repeat("A", 3<<20)))
	}))
	defer server.Close()

	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{
					Key: key, Outcome: doctor.OutcomeAttention, Explanation: "Update available",
					References: []doctor.PublicReference{
						{Kind: "release-notes", Label: "Release Notes", URL: server.URL},
					},
				}}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Configure host fetch to allow http in this local test
	host.Fetch = func(ctx context.Context, address string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, fmt.Errorf("create req: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("do req: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck // test helper
		buf := make([]byte, 2<<20+1024)
		n, _ := resp.Body.Read(buf)
		return buf[:n], nil
	}

	service := NewService(engine, host, doctor.Scope{}, Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	notes, err := service.ReleaseNotes(t.Context(), key)
	if err != nil {
		t.Fatalf("ReleaseNotes: %v", err)
	}
	if notes.URL != server.URL {
		t.Fatalf("notes URL = %q, want %q", notes.URL, server.URL)
	}
	if strings.Contains(notes.Text, "\x1b[31m") {
		t.Fatal("release notes contained raw terminal escape sequence")
	}
	if !notes.Truncated {
		t.Fatal("release notes expected to be truncated")
	}
	if notes.ObservedAt.IsZero() {
		t.Fatal("expected non-zero ObservedAt")
	}
}

func TestReleaseNotesMissingReference(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK, Explanation: "No notes"}}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(engine, host, doctor.Scope{}, Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	_, err = service.ReleaseNotes(t.Context(), key)
	if err == nil {
		t.Fatal("expected error when no release-notes reference is present")
	}
}

var _ = time.Second
