package main

import (
	"fmt"
	"strings"

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
		tui.WithWidth(72),
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

	subtitle := tui.New(
		tui.WithText("Developer Workstation Reliability & Safe Maintenance Engine"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(subtitle)

	rep := d.report.Get()
	if len(rep.Findings) > 0 {
		nOK, nUpdate, nUnknown := 0, 0, 0
		for _, f := range rep.Findings {
			switch f.Outcome {
			case doctor.OutcomeOK:
				nOK++
			case doctor.OutcomeAttention:
				nUpdate++
			default:
				nUnknown++
			}
		}
		statusText := fmt.Sprintf("Last Audit: %d OK · %d Need Update · %d Unknown", nOK, nUpdate, nUnknown)
		card.AddChild(tui.New(
			tui.WithText(statusText),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		))
	}

	card.AddChild(hrLine())

	list := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(0),
	)

	currentSel := d.selectedMenu.Get()
	for i, item := range d.items {
		isSelected := i == currentSel
		prefix := "    "
		style := tui.NewStyle()
		if isSelected {
			prefix = "  ► "
			style = tui.NewStyle().Foreground(catppuccinGreen).Bold()
		}

		line := tui.New(
			tui.WithText(fmt.Sprintf("%s[%s] %s", prefix, item.shortcut, item.label)),
			tui.WithTextStyle(style),
		)
		list.AddChild(line)
	}
	card.AddChild(list)

	card.AddChild(hrLine())

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

	footerText := "[Esc / b: Back to Menu · Enter / d: Details · ↑/↓: Navigate · q: Quit]"
	if nUpdate > 0 {
		footerText = fmt.Sprintf("[Enter / d: View Details · 3: Run Automatic Fix (%d pending) · 2: Manual Steps · b: Menu · q: Quit]", nUpdate)
	}
	nav := tui.New(
		tui.WithText(footerText),
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
	root.AddChild(hrLine())

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
		Automatic bool
	}
	var items []manualItem

	seen := map[string]bool{}

	for _, f := range report.Findings {
		if f.Outcome == doctor.OutcomeOK {
			continue
		}
		for _, a := range f.Actions {
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
				Automatic: a.Mode == doctor.ActionAutomatic,
			})
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
		msg := tui.New(
			tui.WithText("✓ No manual action is needed. All components are current."),
			tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
		)
		contentBox.AddChild(msg)
	} else {
		for i, item := range items {
			stepCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
				tui.WithBorder(tui.BorderRounded),
				tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinMauve)),
				tui.WithPadding(1),
				tui.WithGap(0),
			)
			stepCard.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("Step %d · [%s] %s", i+1, item.Component, item.Title)),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
			))
			if item.Reason != "" && item.Reason != item.Title {
				stepCard.AddChild(tui.New(
					tui.WithText("  Why: "+item.Reason),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
				))
			}
			if item.Command != "" {
				stepCard.AddChild(tui.New(
					tui.WithText("  Run command:"),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				))
				stepCard.AddChild(tui.New(
					tui.WithText("    $ "+item.Command),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
				))
			}
			if item.URL != "" {
				stepCard.AddChild(tui.New(
					tui.WithText("  Documentation: "+item.URL),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue)),
				))
			}
			if item.Automatic {
				stepCard.AddChild(tui.New(
					tui.WithText("  (Tip: You can apply this fix automatically by selecting 'Run automatic fix' [3] from the main menu)"),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
				))
			}
			contentBox.AddChild(stepCard)
		}
	}
	root.AddChild(contentBox)

	root.AddChild(hrLine())

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · 3: Run Automatic Fix · q: Quit]"),
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
				for _, row := range sec.Rows {
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
				for _, row := range sec.Rows {
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
					tui.WithGap(0),
				)
				secBox.AddChild(tui.New(
					tui.WithText(sec.Heading),
					tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
				))
				for _, row := range sec.Rows {
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

	navText := "[Esc / b: Back to Results · ↑/↓: Scroll · q: Quit]"
	hasAutoFix := false
	for _, sec := range det.Sections {
		if sec.Heading == "Proposed Actions" {
			for _, r := range sec.Rows {
				if strings.Contains(r.Label, "[Automatic]") {
					hasAutoFix = true
					break
				}
			}
		}
	}
	if hasAutoFix {
		navText = "[Esc / b: Back to Results · 3 / p: Run Automatic Fix · 2: Manual Steps · ↑/↓: Scroll · q: Quit]"
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

	if d.preparedAction != nil {
		p := d.preparedAction.Preview()
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
				stepsCard.AddChild(tui.New(
					tui.WithText(fmt.Sprintf("  %d. %s: %s", i+1, s.Label, s.Description)),
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
	} else {
		text := d.activeDetail.Get()
		for line := range strings.SplitSeq(text, "\n") {
			contentBox.AddChild(tui.New(tui.WithText(line)))
		}
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	prompt := tui.New(
		tui.WithText("[?] Execute this action? [Press 'y' to confirm · Press 'n' / Esc / any other key to decline]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	root.AddChild(prompt)

	return root
}

func (d *doctorApp) renderActionDone() *tui.Element {
	borderStyle := catppuccinGreen
	titleText := "Action Result & Verification"
	if d.actionReport != nil {
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

	if d.actionReport != nil {
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
	} else {
		text := d.activeDetail.Get()
		for line := range strings.SplitSeq(text, "\n") {
			contentBox.AddChild(tui.New(tui.WithText(line)))
		}
	}
	root.AddChild(contentBox)
	root.AddChild(hrLine())

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Re-audit & Return to Results · q: Quit]"),
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
