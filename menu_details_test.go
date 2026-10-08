package main

import (
	"strings"
	"testing"
	"time"
	"workstation-doctor/internal/doctor"
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

func TestGenericNewCheckDetail(t *testing.T) {
	key := doctor.FindingKey{IntegrationID: "custom-tool", CheckID: "custom-check", InstanceID: "custom-inst"}
	report := doctor.AuditReport{
		Findings: []doctor.Finding{
			{
				Key:         key,
				Outcome:     doctor.OutcomeOK,
				Question:    "Does custom check pass?",
				Explanation: "All criteria met.",
			},
		},
	}

	detail, err := findingDetail(report, key)
	if err != nil {
		t.Fatalf("findingDetail: %v", err)
	}
	text := detailText(detail)
	if !strings.Contains(text, "Does custom check pass?") || !strings.Contains(text, "OK") {
		t.Fatalf("custom check detail failed: %s", text)
	}
}
