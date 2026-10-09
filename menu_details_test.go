package main

import (
	"strings"
	"testing"
	"time"

	"github.com/devararishivian/workstation-doctor/internal/app"
	"github.com/devararishivian/workstation-doctor/internal/doctor"
	"github.com/devararishivian/workstation-doctor/internal/store"
)

func TestIntegrationDetailAbsent(t *testing.T) {
	report := doctor.AuditReport{
		Integrations: []doctor.Integration{
			{ID: "ghostty", Name: "Ghostty", Description: "Terminal emulator", References: []string{"https://ghostty.org"}},
		},
		Discoveries: []doctor.Discovery{
			{Availability: doctor.AvailabilityAbsent, Instances: nil},
		},
	}
	detail, err := integrationDetail(report, "ghostty")
	if err != nil {
		t.Fatalf("integrationDetail: %v", err)
	}
	text := detailText(detail)
	if !strings.Contains(text, "Ghostty") || !strings.Contains(text, "Absent") {
		t.Fatalf("unexpected detail text: %s", text)
	}
}

func TestInstanceDetailEvidence(t *testing.T) {
	instanceFixtureWithoutTimestamp := doctor.Instance{
		ID:            "inst-1",
		IntegrationID: "pi",
		Availability:  doctor.AvailabilityPresent,
		Active:        true,
		Executable:    doctor.Fact{State: doctor.EvidenceKnown, Label: "executable", Value: "/usr/local/bin/pi", Source: "PATH", ObservedAt: time.Now()},
		ResolvedPath:  doctor.Fact{State: doctor.EvidenceKnown, Label: "resolved", Value: "/opt/pi/bin/pi", Source: "symlink", ObservedAt: time.Now()},
		Root:          doctor.Fact{State: doctor.EvidenceKnown, Label: "root", Value: "/opt/pi", Source: "eval", ObservedAt: time.Now()},
		Version:       doctor.Fact{State: doctor.EvidenceKnown, Label: "version", Value: "1.0.0", Source: "cli", ObservedAt: time.Now()},
		Provenance: doctor.Provenance{
			State:   doctor.EvidenceKnown,
			Manager: "brew",
			Package: "pi",
			Root:    "/opt/homebrew",
		},
		InstalledAt: nil, // Missing timestamp
	}

	report := doctor.AuditReport{
		Discoveries: []doctor.Discovery{
			{
				Availability: doctor.AvailabilityPresent,
				Instances:    []doctor.Instance{instanceFixtureWithoutTimestamp},
			},
		},
	}

	detail, err := instanceDetail(report, "inst-1")
	if err != nil {
		t.Fatalf("instanceDetail: %v", err)
	}
	text := detailText(detail)

	if !strings.Contains(text, "Installation time unavailable") || strings.Contains(text, "0001-") {
		t.Fatalf("missing installation evidence became a fabricated date: %s", text)
	}
	if !strings.Contains(text, "/usr/local/bin/pi") || !strings.Contains(text, "/opt/pi/bin/pi") {
		t.Fatalf("expected executable and resolved paths: %s", text)
	}
	if !strings.Contains(text, "brew") || !strings.Contains(text, "/opt/homebrew") {
		t.Fatalf("expected provenance evidence: %s", text)
	}
}

func TestFindingUpdateDetail(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "pi", CheckID: "update", InstanceID: "inst-1"}
	report := doctor.AuditReport{
		Findings: []doctor.Finding{
			{
				Key:         key,
				Outcome:     doctor.OutcomeAttention,
				Question:    "Is pi up to date?",
				Explanation: "Version 1.0.0 is installed; version 2.0.0 is available.",
				Evidence: []doctor.Fact{
					{State: doctor.EvidenceKnown, Label: "installed", Value: "1.0.0", Source: "pi --version", ObservedAt: time.Now()},
					{State: doctor.EvidenceKnown, Label: "latest", Value: "2.0.0", Source: "upstream", ObservedAt: time.Now()},
				},
				References: []doctor.PublicReference{
					{Kind: "release-notes", Label: "Changelog", URL: "https://example.com/changelog"},
				},
				Actions: []doctor.ActionProposal{
					{ID: "update-pi", Label: "Update Pi", Mode: doctor.ActionAutomatic, Reason: "New release"},
				},
			},
		},
	}

	detail, err := findingDetail(report, key)
	if err != nil {
		t.Fatalf("findingDetail: %v", err)
	}
	text := detailText(detail)

	if !strings.Contains(text, "Is pi up to date?") || !strings.Contains(text, "Attention") {
		t.Fatalf("missing question or outcome in detail: %s", text)
	}
	if !strings.Contains(text, "1.0.0") || !strings.Contains(text, "2.0.0") {
		t.Fatalf("missing version evidence in detail: %s", text)
	}
	if !strings.Contains(text, "https://example.com/changelog") {
		t.Fatalf("missing reference URL in detail: %s", text)
	}
}

func TestActionDetail(t *testing.T) {
	prev := app.Preview{
		ActionID:            "fix-1",
		IntegrationID:       "pi",
		CheckID:             "update",
		InstanceID:          "inst-1",
		Mode:                doctor.ActionAutomatic,
		Label:               "Update Pi",
		Reason:              "New version available",
		TargetIDs:           []string{"target-1"},
		TargetVersion:       doctor.Fact{Value: "2.0.0"},
		Steps:               []app.PreviewStep{{Label: "Run upgrade", Description: "Run upgrade"}},
		SideEffects:         []string{"Updates binary"},
		Preconditions:       []doctor.Fact{{Label: "version", Value: "1.0.0"}},
		VerificationCheckID: "verify-check",
	}
	detail := actionDetail(prev)
	text := detailText(detail)

	if !strings.Contains(text, "Update Pi") || !strings.Contains(text, "New version available") {
		t.Fatalf("missing action label or reason: %s", text)
	}
	if !strings.Contains(text, "Run upgrade") {
		t.Fatalf("missing step description: %s", text)
	}
	if !strings.Contains(text, "Updates binary") {
		t.Fatalf("missing side effect: %s", text)
	}
}

func TestHistoryDetail(t *testing.T) {
	rec := store.ActionRecord{
		ID:            "act-123",
		IntegrationID: "pi",
		CheckID:       "update",
		InstanceID:    "inst-1",
		Label:         "Upgrade Pi",
		Kind:          "Automatic",
		Reason:        "Outdated version",
		StartedAt:     time.Now().Add(-1 * time.Hour),
		Plan: []store.PlannedStep{
			{Index: 0, Label: "Step 1", Description: "Download and replace"},
		},
		Finish: &store.ActionFinish{
			Execution:    store.ExecutionCompleted,
			Verification: store.VerificationPassed,
		},
	}
	detail := historyDetail(rec)
	text := detailText(detail)

	if !strings.Contains(text, "act-123") || !strings.Contains(text, "Upgrade Pi") {
		t.Fatalf("missing id or label in history detail: %s", text)
	}
	if !strings.Contains(text, "Completed") || !strings.Contains(text, "Passed") {
		t.Fatalf("missing execution or verification outcome: %s", text)
	}
	if !strings.Contains(text, "Download and replace") {
		t.Fatalf("missing step in history detail: %s", text)
	}
}
