package main

import (
	"fmt"
	"strings"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
)

func (d *doctorApp) Render(_ *tui.App) *tui.Element {
	switch d.mode.Get() {
	case "running":
		return d.renderRunning()
	case "fixing":
		return d.renderRunning()
	case "results":
		return d.renderResults()
	case "manual":
		return d.renderManual()
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
		return d.renderMenu()
	}
}

func (d *doctorApp) renderMenu() *tui.Element {
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
		tui.WithWidth(64),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinBlue)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("workstation-doctor"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	)
	card.AddChild(title)

	if summary := d.lastSummary.Get(); summary != "" {
		sub := tui.New(
			tui.WithText(summary),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
		)
		card.AddChild(sub)
	}

	if msg := d.statusMsg.Get(); msg != "" {
		st := tui.New(
			tui.WithText(msg),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		)
		card.AddChild(st)
	}

	sep := tui.New(
		tui.WithText(strings.Repeat("─", 60)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(sep)

	list := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(0),
	)

	currentSel := d.selectedMenu.Get()
	for i, item := range d.items {
		isSelected := i == currentSel
		prefix := "   "
		style := tui.NewStyle()
		if isSelected {
			prefix = " ► "
			style = tui.NewStyle().Foreground(catppuccinGreen).Bold()
		}

		line := tui.New(
			tui.WithText(fmt.Sprintf("%s[%s] %s", prefix, item.shortcut, item.label)),
			tui.WithTextStyle(style),
		)
		list.AddChild(line)
	}
	card.AddChild(list)

	card.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 60)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	hint := tui.New(
		tui.WithText("Use ↑/↓ or j/k to navigate · Enter to select · q to quit"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(hint)

	wrapper.AddChild(card)
	return wrapper
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
				Status:    "UNKNOWN",
				Note:      "No applicable instance was established within the selected scope.",
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
			nOK, nAttn, nUnk := 0, 0, 0
			for _, f := range findings {
				switch f.Outcome {
				case doctor.OutcomeOK:
					nOK++
				case doctor.OutcomeAttention:
					nAttn++
				default:
					nUnk++
				}
			}
			st := "OK"
			if nAttn > 0 {
				st = "UPDATE"
			} else if nUnk > 0 && nOK == 0 {
				st = "UNKNOWN"
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

	title := tui.New(
		tui.WithText("Workstation Audit Results"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	)
	root.AddChild(title)

	if msg := d.statusMsg.Get(); msg != "" {
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

	sep := tui.New(
		tui.WithText(strings.Repeat("─", 100)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(sep)

	report := d.report.Get()
	rows := buildDashboardRows(report)

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
	nUnknown := 0

	for _, r := range rows {
		switch r.Status {
		case "OK":
			nOK++
		case "UPDATE":
			nUpdate++
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
		default:
			statusStyle = tui.NewStyle().Foreground(catppuccinRed).Bold()
		}

		compStyle := tui.NewStyle().Bold()
		prefix := "  "
		if isSelected {
			prefix = "► "
			compStyle = compStyle.Foreground(catppuccinGreen)
		}

		row.AddChild(tui.New(tui.WithWidth(26), tui.WithText(prefix+trunc(r.Component, 23)), tui.WithTextStyle(compStyle)))
		row.AddChild(tui.New(tui.WithWidth(18), tui.WithText(trunc(r.Installed, 16)), tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim))))
		row.AddChild(tui.New(tui.WithWidth(18), tui.WithText(trunc(r.Latest, 16)), tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim))))
		row.AddChild(tui.New(tui.WithWidth(12), tui.WithText(r.Status), tui.WithTextStyle(statusStyle)))
		row.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText(r.Note)))

		tableBox.AddChild(row)
	}
	root.AddChild(tableBox)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 100)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	// Summary Pills Row
	summaryRow := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithGap(2),
		tui.WithHeight(1),
	)
	summaryRow.AddChild(tui.New(
		tui.WithText(fmt.Sprintf("✓ %d OK", nOK)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
	))
	summaryRow.AddChild(tui.New(
		tui.WithText(fmt.Sprintf("▲ %d NEED UPDATE", nUpdate)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	))
	summaryRow.AddChild(tui.New(
		tui.WithText(fmt.Sprintf("? %d UNKNOWN", nUnknown)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinRed).Bold()),
	))
	root.AddChild(summaryRow)

	nav := tui.New(
		tui.WithText("[Esc / b: Back to Menu · Enter / d: Details · ↑/↓: Navigate · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderManual() *tui.Element {
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
		tui.WithText("Ordered Manual Steps"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(title)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(1),
		tui.WithFlexGrow(1.0),
	)

	report := d.report.Get()
	type manualItem struct {
		Component string
		Title     string
		Reason    string
		Command   string
		URL       string
	}
	var items []manualItem

	seen := map[string]bool{}

	for _, f := range report.Findings {
		if f.Outcome == doctor.OutcomeOK {
			continue
		}
		for _, a := range f.Actions {
			if a.Mode == doctor.ActionManual {
				key := f.Key.CheckID + ":" + a.Label
				if seen[key] {
					continue
				}
				seen[key] = true

				cmd := ""
				if len(a.Steps) > 0 {
					cmd = a.Steps[0].Command.Executable
					if len(a.Steps[0].Command.Args) > 0 {
						cmd += " " + strings.Join(a.Steps[0].Command.Args, " ")
					}
				}
				urlStr := ""
				if len(f.References) > 0 {
					urlStr = f.References[0].URL
				}
				items = append(items, manualItem{
					Component: f.Key.CheckID,
					Title:     a.Label,
					Reason:    a.Reason,
					Command:   cmd,
					URL:       urlStr,
				})
			}
		}
		if f.Outcome == doctor.OutcomeAttention && len(f.Actions) == 0 {
			key := f.Key.CheckID + ":attention"
			if !seen[key] {
				seen[key] = true
				urlStr := ""
				if len(f.References) > 0 {
					urlStr = f.References[0].URL
				}
				items = append(items, manualItem{
					Component: f.Key.CheckID,
					Title:     "Review available update",
					Reason:    f.Explanation,
					URL:       urlStr,
				})
			}
		}
	}

	if len(items) == 0 {
		msg := tui.New(tui.WithText("No manual action is needed. All components are current."))
		contentBox.AddChild(msg)
	} else {
		for i, item := range items {
			stepCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithGap(0),
			)
			stepCard.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("%d. %s: %s", i+1, item.Component, item.Title)),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
			))
			if item.Reason != "" && item.Reason != item.Title {
				stepCard.AddChild(tui.New(
					tui.WithText("   "+item.Reason),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
				))
			}
			if item.Command != "" {
				stepCard.AddChild(tui.New(
					tui.WithText("   $ "+item.Command),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				))
			} else if item.URL != "" {
				stepCard.AddChild(tui.New(
					tui.WithText("   Documentation: "+item.URL),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue)),
				))
			}
			contentBox.AddChild(stepCard)
		}
	}
	root.AddChild(contentBox)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · q: Quit]"),
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
	colHeader.AddChild(tui.New(tui.WithWidth(16), tui.WithText("ACTION ID"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(14), tui.WithText("EXECUTION"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(14), tui.WithText("VERIFICATION"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText("LABEL"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	root.AddChild(colHeader)

	sep := tui.New(
		tui.WithText(strings.Repeat("─", 100)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(sep)

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
		offset := d.scrollOffset.Get()
		if offset > len(actions)-1 && len(actions) > 0 {
			offset = len(actions) - 1
		}
		visible := actions
		if offset > 0 && offset < len(actions) {
			visible = actions[offset:]
		}
		maxDisplay := 20
		if len(visible) > maxDisplay {
			visible = visible[:maxDisplay]
		}

		for _, a := range visible {
			row := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Row),
				tui.WithHeight(1),
			)
			row.AddChild(tui.New(tui.WithWidth(22), tui.WithText(a.StartedAt.Format("2006-01-02 15:04:05")), tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim))))
			row.AddChild(tui.New(tui.WithWidth(16), tui.WithText(trunc(a.ID, 14)), tui.WithTextStyle(tui.NewStyle().Bold())))

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
			row.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText(a.Label)))

			tableBox.AddChild(row)
		}
	}
	root.AddChild(tableBox)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 100)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

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

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

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

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

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

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

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

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

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

	title := tui.New(
		tui.WithText("Inspection Details"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	)
	root.AddChild(title)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1.0),
	)

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
	root.AddChild(contentBox)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	nav := tui.New(
		tui.WithText("[Esc / b: Back to Results · ↑/↓: Scroll · q: Quit]"),
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

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1.0),
	)

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
	maxLines := 22
	if len(visible) > maxLines {
		visible = visible[:maxLines]
	}

	for _, line := range visible {
		contentBox.AddChild(tui.New(tui.WithText(line)))
	}
	root.AddChild(contentBox)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	prompt := tui.New(
		tui.WithText("Execute this action? [Press 'y' to confirm · Press 'n' / Esc / any other key to decline]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(prompt)

	return root
}

func (d *doctorApp) renderActionDone() *tui.Element {
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
		tui.WithText("Action Result & Verification"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
	)
	root.AddChild(title)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	contentBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1.0),
	)

	text := d.activeDetail.Get()
	for line := range strings.SplitSeq(text, "\n") {
		contentBox.AddChild(tui.New(tui.WithText(line)))
	}
	root.AddChild(contentBox)

	root.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 80)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Results · q: Quit]"),
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

	card.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 64)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	msg := tui.New(
		tui.WithText("No automatic fixes are pending. All components are current, or require manual maintenance steps."),
	)
	card.AddChild(msg)

	card.AddChild(tui.New(
		tui.WithText("Select 'Show ordered manual steps' [2] from the menu to review components requiring manual actions."),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	card.AddChild(tui.New(
		tui.WithText(strings.Repeat("─", 64)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	))

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Return to Menu · 2: Show Manual Steps · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(nav)

	wrapper.AddChild(card)
	return wrapper
}
