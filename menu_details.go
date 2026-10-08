package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"workstation-doctor/internal/app"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

// DetailRow represents a single labeled attribute with provenance.
type DetailRow struct {
	Label      string
	Value      string
	Source     string
	ObservedAt string
}

// DetailSection groups related rows under a heading.
type DetailSection struct {
	Heading string
	Rows    []DetailRow
}

// Detail provides a structured representation of an integration, instance, or finding.
type Detail struct {
	Title    string
	Sections []DetailSection
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "Installation time unavailable"
	}
	return t.UTC().Format(time.RFC3339)
}

// detailText renders a human-readable text representation of the detail model.
func detailText(detail Detail) string {
	var sb strings.Builder
	sb.WriteString(detail.Title)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", len(detail.Title)))
	sb.WriteString("\n\n")

	for _, sec := range detail.Sections {
		sb.WriteString(sec.Heading)
		sb.WriteString(":\n")
		sb.WriteString(strings.Repeat("-", len(sec.Heading)+1))
		sb.WriteString("\n")
		for _, row := range sec.Rows {
			fmt.Fprintf(&sb, "  %-24s: %s", row.Label, row.Value)
			if row.Source != "" {
				fmt.Fprintf(&sb, " (source: %s)", row.Source)
			}
			if row.ObservedAt != "" {
				fmt.Fprintf(&sb, " [%s]", row.ObservedAt)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func integrationDetail(report doctor.AuditReport, id string) (Detail, error) {
	var integ *doctor.Integration
	for _, it := range report.Integrations {
		if it.ID == id {
			copied := it
			integ = &copied
			break
		}
	}
	if integ == nil {
		return Detail{}, errors.New("integration not found in report")
	}

	detail := Detail{
		Title: fmt.Sprintf("Integration: %s (%s)", integ.Name, integ.ID),
		Sections: []DetailSection{
			{
				Heading: "Metadata",
				Rows: []DetailRow{
					{Label: "Description", Value: integ.Description},
					{Label: "Scopes", Value: strings.Join(integ.SupportedScopes, ", ")},
					{Label: "References", Value: strings.Join(integ.References, ", ")},
				},
			},
		},
	}

	for _, disc := range report.Discoveries {
		for _, inst := range disc.Instances {
			if inst.IntegrationID == id {
				detail.Sections = append(detail.Sections, DetailSection{
					Heading: "Discovered Instance",
					Rows: []DetailRow{
						{Label: "Instance ID", Value: inst.ID},
						{Label: "Availability", Value: string(inst.Availability)},
						{Label: "Version", Value: inst.Version.Value, Source: inst.Version.Source},
					},
				})
			}
		}
		if disc.Availability == doctor.AvailabilityAbsent && len(disc.Instances) == 0 {
			detail.Sections = append(detail.Sections, DetailSection{
				Heading: "Discovery",
				Rows: []DetailRow{
					{Label: "Availability", Value: string(doctor.AvailabilityAbsent)},
				},
			})
		}
	}

	return detail, nil
}

func instanceDetail(report doctor.AuditReport, id string) (Detail, error) {
	var instance *doctor.Instance
	for _, disc := range report.Discoveries {
		for _, inst := range disc.Instances {
			if inst.ID == id {
				copied := inst
				instance = &copied
				break
			}
		}
	}
	if instance == nil {
		return Detail{}, errors.New("instance not found in report")
	}

	instTime := "Installation time unavailable"
	if instance.InstalledAt != nil && !instance.InstalledAt.At.IsZero() {
		instTime = formatTime(instance.InstalledAt.At)
	}

	provenanceVal := "unresolved"
	if instance.Provenance.Manager != "" {
		provenanceVal = fmt.Sprintf("manager: %s, package: %s, root: %s",
			instance.Provenance.Manager, instance.Provenance.Package, instance.Provenance.Root)
	}

	detail := Detail{
		Title: fmt.Sprintf("Instance: %s (Integration: %s)", instance.ID, instance.IntegrationID),
		Sections: []DetailSection{
			{
				Heading: "Status & Paths",
				Rows: []DetailRow{
					{Label: "Availability", Value: string(instance.Availability)},
					{Label: "Active", Value: fmt.Sprintf("%v", instance.Active)},
					{Label: "Executable", Value: instance.Executable.Value, Source: instance.Executable.Source},
					{Label: "Resolved Path", Value: instance.ResolvedPath.Value, Source: instance.ResolvedPath.Source},
					{Label: "Root Directory", Value: instance.Root.Value, Source: instance.Root.Source},
				},
			},
			{
				Heading: "Provenance & Installation",
				Rows: []DetailRow{
					{Label: "Provenance", Value: provenanceVal},
					{Label: "Version", Value: instance.Version.Value, Source: instance.Version.Source},
					{Label: "Installed At", Value: instTime},
				},
			},
		},
	}

	if len(instance.Configuration) > 0 {
		var cfgRows []DetailRow
		for _, cfg := range instance.Configuration {
			cfgRows = append(cfgRows, DetailRow{
				Label:  cfg.Label,
				Value:  cfg.Value,
				Source: cfg.Source,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Configuration Evidence",
			Rows:    cfgRows,
		})
	}

	return detail, nil
}

func actionDetail(preview app.Preview) Detail {
	detail := Detail{
		Title: fmt.Sprintf("Action Preview: %s", preview.Label),
		Sections: []DetailSection{
			{
				Heading: "Action Overview",
				Rows: []DetailRow{
					{Label: "Action ID", Value: preview.ActionID},
					{Label: "Integration", Value: preview.IntegrationID},
					{Label: "Check", Value: preview.CheckID},
					{Label: "Instance", Value: preview.InstanceID},
					{Label: "Mode", Value: string(preview.Mode)},
					{Label: "Reason", Value: preview.Reason},
					{Label: "Target Version", Value: preview.TargetVersion.Value},
					{Label: "Targets", Value: strings.Join(preview.TargetIDs, ", ")},
				},
			},
		},
	}

	if len(preview.Steps) > 0 {
		var stepRows []DetailRow
		for i, s := range preview.Steps {
			stepRows = append(stepRows, DetailRow{
				Label: fmt.Sprintf("Step %d", i+1),
				Value: s.Description,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Planned Steps",
			Rows:    stepRows,
		})
	}

	if len(preview.SideEffects) > 0 {
		var effectRows []DetailRow
		for _, eff := range preview.SideEffects {
			effectRows = append(effectRows, DetailRow{
				Label: "Side Effect",
				Value: eff,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Expected Side Effects",
			Rows:    effectRows,
		})
	}

	if len(preview.Preconditions) > 0 {
		var preRows []DetailRow
		for _, pre := range preview.Preconditions {
			preRows = append(preRows, DetailRow{
				Label: pre.Label,
				Value: pre.Value,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Preconditions",
			Rows:    preRows,
		})
	}

	if preview.VerificationCheckID != "" {
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Verification",
			Rows: []DetailRow{
				{Label: "Verification Check", Value: preview.VerificationCheckID},
			},
		})
	}

	return detail
}

func historyDetail(record store.ActionRecord) Detail {
	execStr := "Unfinished"
	verStr := "NotPerformed"
	finTimeStr := "In progress"
	obsVer := ""
	safeErr := ""

	if record.Finish != nil {
		execStr = string(record.Finish.Execution)
		verStr = string(record.Finish.Verification)
		finTimeStr = formatTime(record.Finish.FinishedAt)
		obsVer = record.Finish.ObservedVersion
		safeErr = record.Finish.SafeError
	}

	detail := Detail{
		Title: fmt.Sprintf("Action Record: %s (%s)", record.Label, record.ID),
		Sections: []DetailSection{
			{
				Heading: "Execution & Outcome",
				Rows: []DetailRow{
					{Label: "Action ID", Value: record.ID},
					{Label: "Integration", Value: record.IntegrationID},
					{Label: "Check", Value: record.CheckID},
					{Label: "Instance", Value: record.InstanceID},
					{Label: "Kind", Value: record.Kind},
					{Label: "Execution", Value: execStr},
					{Label: "Verification", Value: verStr},
					{Label: "Started At", Value: formatTime(record.StartedAt)},
					{Label: "Finished At", Value: finTimeStr},
					{Label: "Reason", Value: record.Reason},
				},
			},
		},
	}

	if record.InstalledVersion != "" || record.TargetVersion != "" || obsVer != "" {
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Version Information",
			Rows: []DetailRow{
				{Label: "Installed Version", Value: record.InstalledVersion},
				{Label: "Target Version", Value: record.TargetVersion},
				{Label: "Observed Version", Value: obsVer},
			},
		})
	}

	if safeErr != "" {
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Safe Error",
			Rows: []DetailRow{
				{Label: "Error", Value: safeErr},
			},
		})
	}

	if len(record.Plan) > 0 {
		var stepRows []DetailRow
		for _, s := range record.Plan {
			stepRows = append(stepRows, DetailRow{
				Label: fmt.Sprintf("Step %d: %s", s.Index+1, s.Label),
				Value: s.Description,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Plan Steps",
			Rows:    stepRows,
		})
	}

	return detail
}

func actionReportText(rep app.ActionReport) string {
	var sb strings.Builder
	sb.WriteString("Maintenance Execution Report\n")
	sb.WriteString("============================\n\n")
	fmt.Fprintf(&sb, "Record ID:    %s\n", rep.RecordID)
	fmt.Fprintf(&sb, "Execution:    %s\n", rep.Execution)
	fmt.Fprintf(&sb, "Verification: %s\n", rep.Verification)
	if rep.SafeError != "" {
		fmt.Fprintf(&sb, "Error:        %s\n", rep.SafeError)
	}
	if rep.HistoryError != nil {
		fmt.Fprintf(&sb, "History Note: %v\n", rep.HistoryError)
	}
	return sb.String()
}

func findingDetail(report doctor.AuditReport, key doctor.FindingKey) (Detail, error) {
	var finding *doctor.Finding
	for _, f := range report.Findings {
		if f.Key == key {
			copied := f
			finding = &copied
			break
		}
	}
	if finding == nil {
		return Detail{}, errors.New("finding not found in report")
	}

	detail := Detail{
		Title: fmt.Sprintf("Finding: %s / %s", finding.Key.IntegrationID, finding.Key.CheckID),
		Sections: []DetailSection{
			{
				Heading: "Inspection Result",
				Rows: []DetailRow{
					{Label: "Outcome", Value: string(finding.Outcome)},
					{Label: "Question", Value: finding.Question},
					{Label: "Explanation", Value: finding.Explanation},
				},
			},
		},
	}

	if len(finding.Evidence) > 0 {
		var evRows []DetailRow
		for _, ev := range finding.Evidence {
			evRows = append(evRows, DetailRow{
				Label:      ev.Label,
				Value:      ev.Value,
				Source:     ev.Source,
				ObservedAt: formatTime(ev.ObservedAt),
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Evidence",
			Rows:    evRows,
		})
	}

	if len(finding.References) > 0 {
		var refRows []DetailRow
		for _, ref := range finding.References {
			refRows = append(refRows, DetailRow{
				Label: fmt.Sprintf("%s (%s)", ref.Label, ref.Kind),
				Value: ref.URL,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "References",
			Rows:    refRows,
		})
	}

	if len(finding.Actions) > 0 {
		var actRows []DetailRow
		for _, act := range finding.Actions {
			actRows = append(actRows, DetailRow{
				Label: fmt.Sprintf("%s [%s]", act.Label, act.Mode),
				Value: act.Reason,
			})
		}
		detail.Sections = append(detail.Sections, DetailSection{
			Heading: "Proposed Actions",
			Rows:    actRows,
		})
	}

	return detail, nil
}
