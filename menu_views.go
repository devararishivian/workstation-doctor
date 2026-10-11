package main

import (
	"fmt"
	"strings"

	"github.com/devararishivian/workstation-doctor/internal/app"
	"github.com/devararishivian/workstation-doctor/internal/doctor"
	"github.com/devararishivian/workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
)

func hrLine() *tui.Element {
	return tui.New(
		tui.WithHR(),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
}

func formatDashboardNote(note string) string {
	maxLen := 50
	if len(note) <= maxLen {
		return note
	}
	prefixLen := 44
	if idx := strings.LastIndex(note[:prefixLen], " "); idx > 20 {
		prefixLen = idx
	}
	return strings.TrimSpace(note[:prefixLen]) + "... (Enter / d to view details)"
}

func (d *doctorApp) Render(_ *tui.App) *tui.Element {
	switch d.mode.Get() {
	case "checking", "running":
		return d.renderRunning()
	case "fixing":
		return d.renderRunning()
	case "results":
		return d.renderResults()
	case "history":
		return d.renderHistory()
	case "history_detail", "detail":
		return d.renderDetail()
	case "preview_action":
		return d.renderPreviewAction()
	case "action_done":
		return d.renderActionDone()
	case "preview_migration":
		return d.renderPreviewMigration()
	case "migration_done":
		return d.renderMigrationDone()
	case "no_fixes":
		return d.renderNoFixes()
	default:
		return d.renderResults()
	}
}

func (d *doctorApp) renderRunning() *tui.Element {
	frame := spinnerFrames[d.tickCount.Get()%len(spinnerFrames)]
	wrapper := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithJustify(tui.JustifyCenter),
		tui.WithAlign(tui.AlignCenter),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	card := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidth(60),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		tui.WithPadding(1),
		tui.WithGap(1),
		tui.WithAlign(tui.AlignCenter),
	)

	msg := d.statusMsg.Get()
	if msg == "" {
		msg = "Working..."
	}

	spinText := tui.New(
		tui.WithText(fmt.Sprintf("%s %s", frame, msg)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	card.AddChild(spinText)

	hint := tui.New(
		tui.WithText("Running workstation health checks concurrently..."),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(hint)

	wrapper.AddChild(card)
	return wrapper
}

type dashboardRow struct {
	Component string
	Installed string
	Latest    string
	Status    string // "OK", "UPDATE", "UNKNOWN"
	Note      string
	Key       doctor.FindingKey
}

func buildDashboardRows(report doctor.AuditReport) []dashboardRow {
	byCheck := map[string][]doctor.Finding{}
	var checkOrder []string
	for _, f := range report.Findings {
		id := f.Key.CheckID
		if len(byCheck[id]) == 0 {
			checkOrder = append(checkOrder, id)
		}
		byCheck[id] = append(byCheck[id], f)
	}

	if len(report.Checks) > 0 {
		checkOrder = nil
		seen := map[string]bool{}
		for _, c := range report.Checks {
			if !seen[c.ID] {
				seen[c.ID] = true
				checkOrder = append(checkOrder, c.ID)
			}
		}
	}

	var rows []dashboardRow
	for _, cid := range checkOrder {
		findings := byCheck[cid]
		if len(findings) == 0 {
			rows = append(rows, dashboardRow{
				Component: cid,
				Installed: "-",
				Latest:    "-",
				Status:    "N/A",
				Note:      "Tool is not installed or configured in scope.",
				Key:       doctor.FindingKey{CheckID: cid},
			})
			continue
		}
		if len(findings) == 1 {
			f := findings[0]
			inst := "-"
			latest := "-"
			for _, ev := range f.Evidence {
				lbl := strings.ToLower(ev.Label)
				if strings.Contains(lbl, "installed") || strings.Contains(lbl, "version") {
					inst = ev.Value
				}
				if strings.Contains(lbl, "upstream") || strings.Contains(lbl, "release") || strings.Contains(lbl, "latest") {
					latest = ev.Value
				}
			}
			if f.Outcome == doctor.OutcomeOK && latest == "-" && inst != "-" {
				latest = inst
			}
			if inst == "-" && strings.Contains(cid, "config") {
				if f.Outcome == doctor.OutcomeOK {
					inst = "valid"
					latest = "valid"
				} else {
					inst = "invalid"
					latest = "valid"
				}
			}
			st := "UNKNOWN"
			switch f.Outcome {
			case doctor.OutcomeOK:
				st = "OK"
			case doctor.OutcomeAttention:
				st = "UPDATE"
			case doctor.OutcomeNotApplicable:
				st = "N/A"
			}
			rows = append(rows, dashboardRow{
				Component: cid,
				Installed: inst,
				Latest:    latest,
				Status:    st,
				Note:      f.Explanation,
				Key:       f.Key,
			})
		} else {
			nOK, nAttn, nUnk, nNA := 0, 0, 0, 0
			for _, f := range findings {
				switch f.Outcome {
				case doctor.OutcomeOK:
					nOK++
				case doctor.OutcomeAttention:
					nAttn++
				case doctor.OutcomeNotApplicable:
					nNA++
				default:
					nUnk++
				}
			}
			var st string
			switch {
			case nAttn > 0:
				st = "UPDATE"
			case nUnk > 0 && nOK == 0:
				st = "UNKNOWN"
			case nOK == 0 && nAttn == 0 && nNA > 0:
				st = "N/A"
			default:
				st = "OK"
			}
			var unit string
			switch {
			case strings.Contains(cid, "skill"):
				unit = "skills"
			case strings.Contains(cid, "plugin"):
				unit = "plugins"
			case strings.Contains(cid, "pack") || strings.Contains(cid, "brew") || strings.Contains(cid, "npm"):
				unit = "pkgs"
			default:
				unit = "items"
			}
			inst := fmt.Sprintf("%d %s", len(findings), unit)
			latest := "current"
			if nAttn > 0 {
				latest = fmt.Sprintf("%d need update", nAttn)
			}
			note := fmt.Sprintf("all %s are current and valid", unit)
			if nUnk > 0 && nOK == 0 {
				note = findings[0].Explanation
			} else if nAttn > 0 || nUnk > 0 {
				note = fmt.Sprintf("%d ok, %d attention, %d unknown", nOK, nAttn, nUnk)
			}
			rows = append(rows, dashboardRow{
				Component: cid,
				Installed: inst,
				Latest:    latest,
				Status:    st,
				Note:      note,
				Key:       findings[0].Key,
			})
		}
	}
	return rows
}

func (d *doctorApp) renderResults() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	report := d.report.Get()
	rows := buildDashboardRows(report)

	topBar := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithJustify(tui.JustifySpaceBetween),
	)
	topBar.AddChild(tui.New(
		tui.WithText("Workstation Audit Results"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	))
	topBar.AddChild(tui.New(
		tui.WithText(fmt.Sprintf("%d components inspected", len(rows))),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))
	root.AddChild(topBar)

	if msg := d.statusMsg.Get(); msg != "" && !strings.HasPrefix(msg, "Running") {
		root.AddChild(tui.New(
			tui.WithText("▶ "+msg),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		))
	}

	// Column Headers Row
	colHeader := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithHeight(1),
	)
	colHeader.AddChild(tui.New(tui.WithWidth(26), tui.WithText("COMPONENT"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(18), tui.WithText("INSTALLED"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(18), tui.WithText("LATEST"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(12), tui.WithText("STATUS"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText("NOTE"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	root.AddChild(colHeader)

	root.AddChild(hrLine())

	offset := d.scrollOffset.Get()
	if offset > len(rows)-1 && len(rows) > 0 {
		offset = len(rows) - 1
	}

	visible := rows
	if offset > 0 && offset < len(rows) {
		visible = rows[offset:]
	}

	maxDisplay := 22
	if len(visible) > maxDisplay {
		visible = visible[:maxDisplay]
	}

	tableBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1.0),
	)

	nOK := 0
	nUpdate := 0
	nNA := 0
	nUnknown := 0

	for _, r := range rows {
		switch r.Status {
		case "OK":
			nOK++
		case "UPDATE":
			nUpdate++
		case "N/A":
			nNA++
		default:
			nUnknown++
		}
	}

	currentSel := d.selectedResult.Get()

	for idx, r := range visible {
		actualIdx := offset + idx
		isSelected := actualIdx == currentSel

		row := tui.New(
			tui.WithDisplay(tui.DisplayFlex),
			tui.WithDirection(tui.Row),
			tui.WithHeight(1),
		)

		var statusStyle tui.Style
		switch r.Status {
		case "OK":
			statusStyle = tui.NewStyle().Foreground(catppuccinGreen).Bold()
		case "UPDATE":
			statusStyle = tui.NewStyle().Foreground(catppuccinYellow).Bold()
		case "N/A":
			statusStyle = tui.NewStyle().Foreground(catppuccinDim)
		default:
			statusStyle = tui.NewStyle().Foreground(catppuccinRed).Bold()
		}

		var compStyle tui.Style
		var textStyle tui.Style
		prefix := "  "
		if isSelected {
			prefix = "► "
			compStyle = tui.NewStyle().Foreground(catppuccinGreen).Bold()
			textStyle = tui.NewStyle().Bold()
		} else {
			textStyle = tui.NewStyle().Foreground(catppuccinDim)
		}

		formattedNote := formatDashboardNote(r.Note)

		row.AddChild(tui.New(tui.WithWidth(26), tui.WithText(prefix+trunc(r.Component, 23)), tui.WithTextStyle(compStyle)))
		row.AddChild(tui.New(tui.WithWidth(18), tui.WithText(trunc(r.Installed, 16)), tui.WithTextStyle(textStyle)))
		row.AddChild(tui.New(tui.WithWidth(18), tui.WithText(trunc(r.Latest, 16)), tui.WithTextStyle(textStyle)))
		row.AddChild(tui.New(tui.WithWidth(12), tui.WithText(r.Status), tui.WithTextStyle(statusStyle)))
		row.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText(formattedNote), tui.WithTextStyle(textStyle)))

		tableBox.AddChild(row)
	}
	root.AddChild(tableBox)

	root.AddChild(hrLine())

	// Summary Pills Row
	summaryRow := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithGap(2),
		tui.WithHeight(1),
	)
	summaryRow.AddChild(tui.New(
		tui.WithText(fmt.Sprintf("[ OK: %d ]", nOK)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
	))
	if nUpdate > 0 {
		summaryRow.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("[ NEED UPDATE: %d ]", nUpdate)),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
		))
	} else {
		summaryRow.AddChild(tui.New(
			tui.WithText("[ NEED UPDATE: 0 ]"),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
	}
	if nNA > 0 {
		summaryRow.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("[ NOT FOUND: %d ]", nNA)),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
	}
	if nUnknown > 0 {
		summaryRow.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("[ UNKNOWN: %d ]", nUnknown)),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinRed).Bold()),
		))
	} else {
		summaryRow.AddChild(tui.New(
			tui.WithText("[ UNKNOWN: 0 ]"),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
	}
	root.AddChild(summaryRow)

	seenAutoCmd := map[string]bool{}
	nAutoFixes := 0
	for _, f := range report.Findings {
		for _, a := range f.Actions {
			if a.Mode == doctor.ActionAutomatic {
				cmd := ""
				if len(a.Steps) > 0 && len(a.Steps[0].Command.Args) > 0 {
					cmd = a.Steps[0].Command.Executable + " " + strings.Join(a.Steps[0].Command.Args, " ")
				}
				if cmd != "" {
					if seenAutoCmd[cmd] {
						continue
					}
					seenAutoCmd[cmd] = true
				}
				nAutoFixes++
			}
		}
	}

	footerText := "[Esc / b: Back to Menu · Enter / d: Details · ↑/↓: Navigate · q: Quit]"
	if nAutoFixes > 0 {
		footerText = fmt.Sprintf("[Enter / d: View Details · 3: Run Automatic Fix (%d pending) · 2: Manual Steps · b: Menu · q: Quit]", nAutoFixes)
	} else if nUpdate > 0 {
		footerText = "[Enter / d: View Details · 2: Manual Steps · b: Menu · q: Quit]"
	}
	nav := tui.New(
		tui.WithText(footerText),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderHistory() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	title := tui.New(
		tui.WithText("Maintenance Action History"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	)
	root.AddChild(title)

	// Column Headers Row
	colHeader := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithHeight(1),
	)
	colHeader.AddChild(tui.New(tui.WithWidth(22), tui.WithText("STARTED"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(28), tui.WithText("ACTION ID"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(14), tui.WithText("EXECUTION"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(14), tui.WithText("VERIFICATION"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText("LABEL"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	root.AddChild(colHeader)

	root.AddChild(hrLine())

	actions := d.historyActions.Get()
	tableBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1.0),
	)

	if len(actions) == 0 {
		empty := tui.New(tui.WithText("No maintenance actions recorded yet."))
		tableBox.AddChild(empty)
	} else {
		currentSel := max(0, min(d.selectedResult.Get(), len(actions)-1))

		maxDisplay := 20
		offset := 0
		if currentSel >= maxDisplay {
			offset = currentSel - maxDisplay + 1
		}
		end := min(offset+maxDisplay, len(actions))
		visible := actions[offset:end]

		for idx, a := range visible {
			actualIdx := offset + idx
			isSelected := actualIdx == currentSel

			row := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Row),
				tui.WithHeight(1),
			)

			prefix := "  "
			var textStyle tui.Style
			var dateStyle tui.Style
			if isSelected {
				prefix = "► "
				dateStyle = tui.NewStyle().Foreground(catppuccinGreen).Bold()
				textStyle = tui.NewStyle().Bold()
			} else {
				dateStyle = tui.NewStyle().Foreground(catppuccinDim)
				textStyle = tui.NewStyle().Foreground(catppuccinDim)
			}

			row.AddChild(tui.New(tui.WithWidth(22), tui.WithText(prefix+a.StartedAt.Local().Format("2006-01-02 15:04:05")), tui.WithTextStyle(dateStyle)))
			row.AddChild(tui.New(tui.WithWidth(28), tui.WithText(trunc(a.ID, 26)), tui.WithTextStyle(textStyle)))

			execStr := "Running"
			execStyle := tui.NewStyle().Foreground(catppuccinYellow)
			verStr := ""
			verStyle := tui.NewStyle().Foreground(catppuccinDim)

			if a.Finish != nil {
				execStr = string(a.Finish.Execution)
				switch a.Finish.Execution {
				case store.ExecutionCompleted:
					execStyle = tui.NewStyle().Foreground(catppuccinGreen).Bold()
				case store.ExecutionFailed, store.ExecutionCanceled:
					execStyle = tui.NewStyle().Foreground(catppuccinRed).Bold()
				}

				verStr = string(a.Finish.Verification)
				switch a.Finish.Verification {
				case store.VerificationPassed:
					verStyle = tui.NewStyle().Foreground(catppuccinGreen)
				case store.VerificationFailed:
					verStyle = tui.NewStyle().Foreground(catppuccinRed)
				}
			}

			row.AddChild(tui.New(tui.WithWidth(14), tui.WithText(execStr), tui.WithTextStyle(execStyle)))
			row.AddChild(tui.New(tui.WithWidth(14), tui.WithText(verStr), tui.WithTextStyle(verStyle)))
			row.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText(a.Label), tui.WithTextStyle(textStyle)))

			tableBox.AddChild(row)
		}
	}
	root.AddChild(tableBox)

	root.AddChild(hrLine())

	nav := tui.New(
		tui.WithText("[Esc / b / m: Back to Menu · Enter / d: View Details · ↑/↓: Scroll · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderPreviewMigration() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	title := tui.New(
		tui.WithText("Legacy History Migration Preview"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(title)
	root.AddChild(hrLine())

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(1),
		tui.WithFlexGrow(1.0),
	)

	if d.migrationPreview != nil {
		p := d.migrationPreview
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Source Database:      %s", p.Source))))
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Destination Database: %s", p.Destination))))
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Eligible Actions:     %d", p.Actions))))
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Excluded Check Runs:  %d (audit snapshots excluded)", p.CheckRuns))))
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Source Fingerprint:   %s", p.SourceFingerprint))))
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	prompt := tui.New(
		tui.WithText("Proceed with legacy history import? [Press 'y' to confirm · Press 'n' / Esc to cancel]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(prompt)

	return root
}

func (d *doctorApp) renderMigrationDone() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinGreen)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	title := tui.New(
		tui.WithText("Migration Completed"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
	)
	root.AddChild(title)
	root.AddChild(hrLine())

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(1),
		tui.WithFlexGrow(1.0),
	)

	if d.migrationResult != nil {
		r := d.migrationResult
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Imported Actions: %d", r.Imported))))
		contentBox.AddChild(tui.New(tui.WithText(fmt.Sprintf("Preserved Source: %s", r.PreservedSource))))
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Return to Menu · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderDetail() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	det := Detail{}
	if d.currentDetail != nil {
		det = d.currentDetail.Get()
	}

	titleText := det.Title
	if titleText == "" {
		titleText = "Inspection Details"
	}

	topBar := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithJustify(tui.JustifySpaceBetween),
	)
	topBar.AddChild(tui.New(
		tui.WithText(titleText),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	))
	topBar.AddChild(tui.New(
		tui.WithText("workstation-doctor"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))
	root.AddChild(topBar)
	root.AddChild(hrLine())

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(1),
		tui.WithFlexGrow(1.0),
	)

	if len(det.Sections) == 0 {
		text := d.activeDetail.Get()
		lines := strings.Split(text, "\n")
		offset := d.scrollOffset.Get()
		if offset > len(lines)-1 && len(lines) > 0 {
			offset = len(lines) - 1
		}
		visible := lines
		if offset > 0 && offset < len(lines) {
			visible = lines[offset:]
		}
		maxLines := 24
		if len(visible) > maxLines {
			visible = visible[:maxLines]
		}
		for _, line := range visible {
			contentBox.AddChild(tui.New(
				tui.WithText(line),
			))
		}
	} else {
		for _, sec := range det.Sections {
			switch sec.Heading {
			case "Summary":
				summaryCard := tui.New(
					tui.WithDisplay(tui.DisplayFlex),
					tui.WithDirection(tui.Row),
					tui.WithGap(2),
					tui.WithPadding(1),
					tui.WithBorder(tui.BorderRounded),
					tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinMauve)),
				)
				for _, row := range sec.Rows {
					valStyle := tui.NewStyle().Bold()
					switch row.Label {
					case "OK":
						valStyle = valStyle.Foreground(catppuccinGreen)
					case "Attention / Updates":
						if row.Value != "0" {
							valStyle = valStyle.Foreground(catppuccinYellow)
						}
					case "Unknown":
						if row.Value != "0" {
							valStyle = valStyle.Foreground(catppuccinRed)
						}
					default:
						valStyle = valStyle.Foreground(catppuccinBlue)
					}
					pill := tui.New(
						tui.WithText(fmt.Sprintf("%s: %s", row.Label, row.Value)),
						tui.WithTextStyle(valStyle),
					)
					summaryCard.AddChild(pill)
				}
				contentBox.AddChild(summaryCard)

			case "Update Available":
				updateBox := tui.New(
					tui.WithDisplay(tui.DisplayFlex),
					tui.WithDirection(tui.Column),
					tui.WithGap(1),
					tui.WithPadding(1),
					tui.WithBorder(tui.BorderRounded),
					tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				)
				updateBox.AddChild(tui.New(
					tui.WithText("Update Available"),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
				))
				uRows := sec.Rows
				if len(uRows) > 8 {
					offset := d.scrollOffset.Get()
					if offset > len(uRows)-1 && len(uRows) > 0 {
						offset = len(uRows) - 1
					}
					if offset < 0 {
						offset = 0
					}
					visible := uRows[offset:]
					if len(visible) > 8 {
						visible = visible[:8]
					}
					updateBox.AddChild(tui.New(
						tui.WithText(fmt.Sprintf("  (Showing %d–%d of %d · Use ↑/↓ to scroll)", offset+1, offset+len(visible), len(uRows))),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
					))
					uRows = visible
				}
				for _, row := range uRows {
					switch {
					case strings.Contains(row.Label, "Manual command"):
						cmdRow := tui.New(
							tui.WithDisplay(tui.DisplayFlex),
							tui.WithDirection(tui.Column),
							tui.WithGap(0),
						)
						cmdRow.AddChild(tui.New(
							tui.WithText("  Manual command:"),
							tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
						))
						cmdRow.AddChild(tui.New(
							tui.WithText("  $ "+row.Value),
							tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
						))
						updateBox.AddChild(cmdRow)
					case strings.HasPrefix(row.Label, "Fix:"):
						fixRow := tui.New(
							tui.WithDisplay(tui.DisplayFlex),
							tui.WithDirection(tui.Column),
							tui.WithGap(0),
						)
						fixRow.AddChild(tui.New(
							tui.WithText("• "+row.Label),
							tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
						))
						if row.Value != "" {
							fixRow.AddChild(tui.New(
								tui.WithText("  "+row.Value),
								tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
							))
						}
						updateBox.AddChild(fixRow)
					case row.Label == "Documentation":
						docRow := tui.New(
							tui.WithDisplay(tui.DisplayFlex),
							tui.WithDirection(tui.Row),
							tui.WithGap(1),
						)
						docRow.AddChild(tui.New(
							tui.WithText("  Documentation:"),
							tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
						))
						docRow.AddChild(tui.New(
							tui.WithText(row.Value),
							tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue)),
						))
						updateBox.AddChild(docRow)
					default:
						itemRow := tui.New(
							tui.WithDisplay(tui.DisplayFlex),
							tui.WithDirection(tui.Column),
							tui.WithGap(0),
						)
						itemRow.AddChild(tui.New(
							tui.WithText("• "+row.Label),
							tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
						))
						if row.Value != "" {
							itemRow.AddChild(tui.New(
								tui.WithText("  "+row.Value),
								tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
							))
						}
						updateBox.AddChild(itemRow)
					}
				}
				contentBox.AddChild(updateBox)

			case "Items Requiring Attention":
				attnBox := tui.New(
					tui.WithDisplay(tui.DisplayFlex),
					tui.WithDirection(tui.Column),
					tui.WithGap(1),
					tui.WithPadding(1),
					tui.WithBorder(tui.BorderRounded),
					tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				)
				attnBox.AddChild(tui.New(
					tui.WithText("[!] Items Requiring Attention"),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
				))
				rows := sec.Rows
				if len(rows) > 4 {
					offset := d.scrollOffset.Get()
					if offset > len(rows)-1 && len(rows) > 0 {
						offset = len(rows) - 1
					}
					if offset < 0 {
						offset = 0
					}
					visible := rows[offset:]
					if len(visible) > 4 {
						visible = visible[:4]
					}
					attnBox.AddChild(tui.New(
						tui.WithText(fmt.Sprintf("  (Showing %d–%d of %d · Use ↑/↓ to scroll)", offset+1, offset+len(visible), len(rows))),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
					))
					rows = visible
				}
				for _, row := range rows {
					itemRow := tui.New(
						tui.WithDisplay(tui.DisplayFlex),
						tui.WithDirection(tui.Column),
						tui.WithGap(0),
					)
					itemRow.AddChild(tui.New(
						tui.WithText("• "+row.Label),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
					))
					itemRow.AddChild(tui.New(
						tui.WithText("  "+row.Value),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
					))
					attnBox.AddChild(itemRow)
				}
				contentBox.AddChild(attnBox)

			case "Proposed Actions":
				actBox := tui.New(
					tui.WithDisplay(tui.DisplayFlex),
					tui.WithDirection(tui.Column),
					tui.WithGap(1),
					tui.WithPadding(1),
					tui.WithBorder(tui.BorderRounded),
					tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinGreen)),
				)
				actBox.AddChild(tui.New(
					tui.WithText("[Action] Proposed Actions"),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
				))
				pRows := sec.Rows
				if len(pRows) > 4 {
					offset := d.scrollOffset.Get()
					if offset > len(pRows)-1 && len(pRows) > 0 {
						offset = len(pRows) - 1
					}
					if offset < 0 {
						offset = 0
					}
					visible := pRows[offset:]
					if len(visible) > 4 {
						visible = visible[:4]
					}
					actBox.AddChild(tui.New(
						tui.WithText(fmt.Sprintf("  (Showing %d–%d of %d · Use ↑/↓ to scroll)", offset+1, offset+len(visible), len(pRows))),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
					))
					pRows = visible
				}
				for _, row := range pRows {
					itemRow := tui.New(
						tui.WithDisplay(tui.DisplayFlex),
						tui.WithDirection(tui.Column),
						tui.WithGap(0),
					)
					itemRow.AddChild(tui.New(
						tui.WithText("• "+row.Label),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
					))
					itemRow.AddChild(tui.New(
						tui.WithText("  "+row.Value),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
					))
					actBox.AddChild(itemRow)
				}
				contentBox.AddChild(actBox)

			default:
				secBox := tui.New(
					tui.WithDisplay(tui.DisplayFlex),
					tui.WithDirection(tui.Column),
					tui.WithBorder(tui.BorderRounded),
					tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
					tui.WithPadding(1),
					tui.WithGap(0),
				)
				secBox.AddChild(tui.New(
					tui.WithText(sec.Heading),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
				))
				dRows := sec.Rows
				if len(dRows) > 8 {
					offset := d.scrollOffset.Get()
					if offset > len(dRows)-1 && len(dRows) > 0 {
						offset = len(dRows) - 1
					}
					if offset < 0 {
						offset = 0
					}
					visible := dRows[offset:]
					if len(visible) > 8 {
						visible = visible[:8]
					}
					secBox.AddChild(tui.New(
						tui.WithText(fmt.Sprintf("  (Showing %d–%d of %d · Use ↑/↓ to scroll)", offset+1, offset+len(visible), len(dRows))),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
					))
					dRows = visible
				}
				for _, row := range dRows {
					rElem := tui.New(
						tui.WithDisplay(tui.DisplayFlex),
						tui.WithDirection(tui.Row),
						tui.WithGap(2),
						tui.WithHeight(1),
					)
					rElem.AddChild(tui.New(
						tui.WithWidth(46),
						tui.WithText("  "+trunc(row.Label, 44)),
						tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinBlue)),
					))
					valStr := row.Value
					if row.Source != "" {
						valStr += fmt.Sprintf(" [%s]", row.Source)
					}
					rElem.AddChild(tui.New(
						tui.WithFlexGrow(1.0),
						tui.WithText(valStr),
						tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
					))
					secBox.AddChild(rElem)
				}
				contentBox.AddChild(secBox)
			}
		}
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	navText := "[Esc/b: Back · ↑/↓: Scroll · q: Quit]"
	hasAutoFix := false
	for _, sec := range det.Sections {
		if sec.Heading == "Update Available" || sec.Heading == "Proposed Actions" {
			for _, r := range sec.Rows {
				if strings.Contains(r.Label, "[Automatic]") {
					hasAutoFix = true
					break
				}
			}
		}
	}
	if hasAutoFix {
		navText = "[Esc/b: Back · f: Fix this component · ↑/↓: Scroll · q: Quit]"
	}
	nav := tui.New(
		tui.WithText(navText),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderPreviewAction() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	title := tui.New(
		tui.WithText("Confirmed Action Preview"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(title)
	root.AddChild(hrLine())

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(1),
		tui.WithFlexGrow(1.0),
	)

	preps := d.preparedActions
	if len(preps) == 0 && d.preparedAction != nil {
		preps = []app.PreparedAction{*d.preparedAction}
	}

	switch {
	case len(preps) > 1:
		compCounts := map[string]int{}
		var compOrder []string
		for _, prep := range preps {
			cid := prep.Preview().CheckID
			if compCounts[cid] == 0 {
				compOrder = append(compOrder, cid)
			}
			compCounts[cid]++
		}
		var compSummaries []string
		for _, cid := range compOrder {
			compSummaries = append(compSummaries, fmt.Sprintf("%s (%d)", cid, compCounts[cid]))
		}

		infoCard := tui.New(
			tui.WithDisplay(tui.DisplayFlex),
			tui.WithDirection(tui.Column),
			tui.WithBorder(tui.BorderRounded),
			tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
			tui.WithPadding(1),
			tui.WithGap(0),
		)
		infoCard.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("Action: Batch Maintenance Fix (%d actions)", len(preps))),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
		))
		infoCard.AddChild(tui.New(
			tui.WithText("Components: "+strings.Join(compSummaries, ", ")),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
		infoCard.AddChild(tui.New(
			tui.WithText("Reason: Apply all available automatic maintenance updates across components"),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
		contentBox.AddChild(infoCard)

		type batchStepItem struct {
			Component string
			Label     string
		}
		var allSteps []batchStepItem
		for _, prep := range preps {
			p := prep.Preview()
			for _, st := range p.Steps {
				allSteps = append(allSteps, batchStepItem{
					Component: p.CheckID,
					Label:     st.Label,
				})
			}
		}

		if len(allSteps) > 0 {
			stepsCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithBorder(tui.BorderRounded),
				tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinGreen)),
				tui.WithPadding(1),
				tui.WithGap(0),
			)
			stepsCard.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("Planned Steps (%d total):", len(allSteps))),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
			))

			offset := d.scrollOffset.Get()
			if offset > len(allSteps)-1 && len(allSteps) > 0 {
				offset = len(allSteps) - 1
			}
			if offset < 0 {
				offset = 0
			}
			maxDisplaySteps := 5
			visibleSteps := allSteps[offset:]
			if len(visibleSteps) > maxDisplaySteps {
				visibleSteps = visibleSteps[:maxDisplaySteps]
			}

			if len(allSteps) > maxDisplaySteps {
				stepsCard.AddChild(tui.New(
					tui.WithText(fmt.Sprintf("  (Showing %d–%d of %d steps · Use ↑/↓ to scroll)",
						offset+1, offset+len(visibleSteps), len(allSteps))),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				))
			}

			for i, s := range visibleSteps {
				stepsCard.AddChild(tui.New(
					tui.WithText(fmt.Sprintf("  %d. [%s] %s", offset+i+1, s.Component, s.Label)),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen)),
				))
			}
			contentBox.AddChild(stepsCard)
		}

		seenEff := map[string]bool{}
		var allEff []string
		for _, prep := range preps {
			for _, eff := range prep.Preview().SideEffects {
				if !seenEff[eff] {
					seenEff[eff] = true
					allEff = append(allEff, eff)
				}
			}
		}

		if len(allEff) > 0 {
			effCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithBorder(tui.BorderRounded),
				tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				tui.WithPadding(1),
				tui.WithGap(0),
			)
			effCard.AddChild(tui.New(
				tui.WithText("Side Effects & Permissions:"),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
			))
			for _, eff := range allEff {
				effCard.AddChild(tui.New(
					tui.WithText("  • "+eff),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
				))
			}
			contentBox.AddChild(effCard)
		}
	case len(preps) == 1:
		p := preps[0].Preview()
		var cleanTargets []string
		for _, tid := range p.TargetIDs {
			if len(tid) > 20 && strings.Contains(tid, ":") {
				parts := strings.Split(tid, ":")
				cleanTargets = append(cleanTargets, parts[0])
			} else {
				cleanTargets = append(cleanTargets, tid)
			}
		}
		targetDisplay := strings.Join(cleanTargets, ", ")
		if targetDisplay == "" {
			targetDisplay = p.CheckID
		}

		infoCard := tui.New(
			tui.WithDisplay(tui.DisplayFlex),
			tui.WithDirection(tui.Column),
			tui.WithBorder(tui.BorderRounded),
			tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
			tui.WithPadding(1),
			tui.WithGap(0),
		)
		infoCard.AddChild(tui.New(
			tui.WithText("Action: "+p.Label),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
		))
		infoCard.AddChild(tui.New(
			tui.WithText("Component: "+p.CheckID+" ("+targetDisplay+")"),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
		infoCard.AddChild(tui.New(
			tui.WithText("Reason: "+p.Reason),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
		contentBox.AddChild(infoCard)

		if len(p.Steps) > 0 {
			stepsCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithBorder(tui.BorderRounded),
				tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinGreen)),
				tui.WithPadding(1),
				tui.WithGap(0),
			)
			stepsCard.AddChild(tui.New(
				tui.WithText("Planned Steps:"),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
			))
			for i, s := range p.Steps {
				desc := s.Label
				if s.Description != "" && s.Description != s.Label {
					desc = fmt.Sprintf("%s: %s", s.Label, s.Description)
				}
				stepsCard.AddChild(tui.New(
					tui.WithText(fmt.Sprintf("  %d. %s", i+1, desc)),
				))
			}
			contentBox.AddChild(stepsCard)
		}

		if len(p.SideEffects) > 0 {
			effCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithBorder(tui.BorderRounded),
				tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				tui.WithPadding(1),
				tui.WithGap(0),
			)
			effCard.AddChild(tui.New(
				tui.WithText("Side Effects & Permissions:"),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
			))
			for _, eff := range p.SideEffects {
				effCard.AddChild(tui.New(
					tui.WithText("  • "+eff),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
				))
			}
			contentBox.AddChild(effCard)
		}
	default:
		text := d.activeDetail.Get()
		for line := range strings.SplitSeq(text, "\n") {
			contentBox.AddChild(tui.New(tui.WithText(line)))
		}
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	promptText := "[?] Execute this action? [Press 'y' to confirm · Press 'n' / Esc / any other key to decline]"
	if len(preps) > 1 {
		promptText = fmt.Sprintf("[?] Execute all %d actions? [Press 'y' to confirm · Press 'n' / Esc to decline · ↑/↓: Scroll]", len(preps))
	}
	prompt := tui.New(
		tui.WithText(promptText),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(prompt)

	return root
}

func (d *doctorApp) renderActionDone() *tui.Element {
	borderStyle := catppuccinGreen
	titleText := "Action Result & Verification"
	nSuccess := 0
	nFailed := 0
	if len(d.batchReports) > 0 {
		for _, r := range d.batchReports {
			if r.Execution == store.ExecutionCompleted {
				nSuccess++
			} else {
				nFailed++
			}
		}
		if nFailed > 0 {
			borderStyle = catppuccinRed
			titleText = "Action Execution Incomplete"
		}
	} else if d.actionReport != nil {
		if d.actionReport.Execution == store.ExecutionFailed || d.actionReport.Execution == store.ExecutionCanceled || d.actionReport.SafeError != "" {
			borderStyle = catppuccinRed
			titleText = "Action Execution Failed"
		}
	}

	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(borderStyle)),
		tui.WithPadding(1),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	title := tui.New(
		tui.WithText(titleText),
		tui.WithTextStyle(tui.NewStyle().Foreground(borderStyle).Bold()),
	)
	root.AddChild(title)
	root.AddChild(hrLine())

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(1),
		tui.WithFlexGrow(1.0),
	)

	switch {
	case len(d.batchReports) > 0:
		card := tui.New(
			tui.WithDisplay(tui.DisplayFlex),
			tui.WithDirection(tui.Column),
			tui.WithBorder(tui.BorderRounded),
			tui.WithBorderStyle(tui.NewStyle().Foreground(borderStyle)),
			tui.WithPadding(1),
			tui.WithGap(0),
		)
		card.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("Summary: %d / %d actions executed successfully", nSuccess, len(d.batchReports))),
			tui.WithTextStyle(tui.NewStyle().Bold().Foreground(borderStyle)),
		))

		offset := d.scrollOffset.Get()
		if offset > len(d.batchReports)-1 && len(d.batchReports) > 0 {
			offset = len(d.batchReports) - 1
		}
		if offset < 0 {
			offset = 0
		}
		maxDisplayReps := 5
		visibleReps := d.batchReports[offset:]
		if len(visibleReps) > maxDisplayReps {
			visibleReps = visibleReps[:maxDisplayReps]
		}

		if len(d.batchReports) > maxDisplayReps {
			card.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("  (Showing %d–%d of %d · Use ↑/↓ to scroll)",
					offset+1, offset+len(visibleReps), len(d.batchReports))),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
			))
		}

		for i, rep := range visibleReps {
			idx := offset + i + 1
			mark := "✓"
			color := catppuccinGreen
			if rep.Execution != store.ExecutionCompleted {
				mark = "✗"
				color = catppuccinRed
			}
			recText := rep.RecordID
			if recText == "" {
				recText = fmt.Sprintf("Action %d", idx)
			}
			card.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("  %s %s: Execution %s · Verification %s", mark, recText, rep.Execution, rep.Verification)),
				tui.WithTextStyle(tui.NewStyle().Foreground(color)),
			))
			if rep.SafeError != "" {
				card.AddChild(tui.New(
					tui.WithText(fmt.Sprintf("    Error: %s", rep.SafeError)),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinRed)),
				))
			}
		}
		contentBox.AddChild(card)

		contentBox.AddChild(tui.New(
			tui.WithText("Press Enter or 'b' to re-run audit and return to the results dashboard."),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
	case d.actionReport != nil:
		rep := *d.actionReport
		card := tui.New(
			tui.WithDisplay(tui.DisplayFlex),
			tui.WithDirection(tui.Column),
			tui.WithBorder(tui.BorderRounded),
			tui.WithBorderStyle(tui.NewStyle().Foreground(borderStyle)),
			tui.WithPadding(1),
			tui.WithGap(0),
		)
		execStyle := tui.NewStyle().Foreground(catppuccinGreen).Bold()
		if rep.Execution == store.ExecutionFailed || rep.Execution == store.ExecutionCanceled {
			execStyle = tui.NewStyle().Foreground(catppuccinRed).Bold()
		}
		card.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("Execution:    %s", rep.Execution)),
			tui.WithTextStyle(execStyle),
		))
		card.AddChild(tui.New(
			tui.WithText(fmt.Sprintf("Verification: %s", rep.Verification)),
			tui.WithTextStyle(tui.NewStyle().Bold()),
		))
		if rep.RecordID != "" {
			card.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("Record ID:    %s", rep.RecordID)),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
			))
		}
		if rep.SafeError != "" {
			card.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("Error Details: %s", rep.SafeError)),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinRed).Bold()),
			))
		}
		contentBox.AddChild(card)

		contentBox.AddChild(tui.New(
			tui.WithText("Press Enter or 'b' to return to the results dashboard."),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		))
	default:
		text := d.activeDetail.Get()
		for line := range strings.SplitSeq(text, "\n") {
			contentBox.AddChild(tui.New(tui.WithText(line)))
		}
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Re-audit & Return to Results · ↑/↓: Scroll · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderNoFixes() *tui.Element {
	wrapper := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithJustify(tui.JustifyCenter),
		tui.WithAlign(tui.AlignCenter),
		tui.WithHeightPercent(100.0),
		tui.WithWidthPercent(100.0),
	)

	card := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithWidth(68),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinGreen)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("Automatic Maintenance Notice"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
	)
	card.AddChild(title)
	card.AddChild(hrLine())

	msg := tui.New(
		tui.WithText("No automatic fixes are pending. All components are current, or require manual maintenance steps."),
	)
	card.AddChild(msg)

	card.AddChild(tui.New(
		tui.WithText("Select 'Show ordered manual steps' [2] from the menu to review components requiring manual actions."),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))
	card.AddChild(hrLine())

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Return to Menu · 2: Show Manual Steps · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(nav)

	wrapper.AddChild(card)
	return wrapper
}
