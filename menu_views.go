package main

import (
	"fmt"
	"strings"
	"workstation-doctor/internal/doctor"

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
	case "confirm_fix":
		return d.renderConfirmFix()
	case "fix_done":
		return d.renderFixDone()
	case "detail":
		return d.renderDetail()
	case "preview_action":
		return d.renderPreviewAction()
	case "action_done":
		return d.renderActionDone()
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

	headerText := "Workstation Audit Results"
	title := tui.New(
		tui.WithText(headerText),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinBlue).Bold()),
	)
	root.AddChild(title)

	// Column Headers Row
	colHeader := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Row),
		tui.WithHeight(1),
	)
	colHeader.AddChild(tui.New(tui.WithWidth(26), tui.WithText("INTEGRATION"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(24), tui.WithText("CHECK"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithWidth(14), tui.WithText("OUTCOME"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	colHeader.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText("EXPLANATION"), tui.WithTextStyle(tui.NewStyle().Bold().Foreground(catppuccinMauve))))
	root.AddChild(colHeader)

	sep := tui.New(
		tui.WithText(strings.Repeat("─", 100)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	root.AddChild(sep)

	report := d.report.Get()
	findings := report.Findings
	offset := d.scrollOffset.Get()
	if offset > len(findings)-1 && len(findings) > 0 {
		offset = len(findings) - 1
	}

	visible := findings
	if offset > 0 && offset < len(findings) {
		visible = findings[offset:]
	}

	maxDisplay := 20
	if len(visible) > maxDisplay {
		visible = visible[:maxDisplay]
	}

	tableBox := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithFlexGrow(1.0),
	)

	nOK := 0
	nAttention := 0
	nUnknown := 0

	for _, f := range findings {
		switch f.Outcome {
		case doctor.OutcomeOK:
			nOK++
		case doctor.OutcomeAttention:
			nAttention++
		default:
			nUnknown++
		}
	}

	for _, f := range visible {
		row := tui.New(
			tui.WithDisplay(tui.DisplayFlex),
			tui.WithDirection(tui.Row),
			tui.WithHeight(1),
		)

		var outcomeStyle tui.Style
		switch f.Outcome {
		case doctor.OutcomeOK:
			outcomeStyle = tui.NewStyle().Foreground(catppuccinGreen).Bold()
		case doctor.OutcomeAttention:
			outcomeStyle = tui.NewStyle().Foreground(catppuccinYellow).Bold()
		default:
			outcomeStyle = tui.NewStyle().Foreground(catppuccinRed).Bold()
		}

		compStyle := tui.NewStyle().Bold()
		row.AddChild(tui.New(tui.WithWidth(26), tui.WithText(trunc(f.Key.IntegrationID, 24)), tui.WithTextStyle(compStyle)))
		row.AddChild(tui.New(tui.WithWidth(24), tui.WithText(trunc(f.Key.CheckID, 22)), tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim))))
		row.AddChild(tui.New(tui.WithWidth(14), tui.WithText(string(f.Outcome)), tui.WithTextStyle(outcomeStyle)))
		row.AddChild(tui.New(tui.WithFlexGrow(1.0), tui.WithText(f.Explanation)))

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
		tui.WithText(fmt.Sprintf("▲ %d ATTENTION", nAttention)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	))
	summaryRow.AddChild(tui.New(
		tui.WithText(fmt.Sprintf("? %d OTHER/UNKNOWN", nUnknown)),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinRed).Bold()),
	))
	root.AddChild(summaryRow)

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · ↑/↓: Scroll · q: Quit]"),
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
	var manualActions []doctor.ActionProposal
	for _, f := range report.Findings {
		for _, a := range f.Actions {
			if a.Mode == doctor.ActionManual {
				manualActions = append(manualActions, a)
			}
		}
	}

	if len(manualActions) == 0 {
		msg := tui.New(tui.WithText("No manual action is needed. All components are current."))
		contentBox.AddChild(msg)
	} else {
		for i, a := range manualActions {
			stepCard := tui.New(
				tui.WithDisplay(tui.DisplayFlex),
				tui.WithDirection(tui.Column),
			)
			stepCard.AddChild(tui.New(
				tui.WithText(fmt.Sprintf("%d. %s (%s)", i+1, a.Label, a.Reason)),
				tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinMauve).Bold()),
			))
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

	empty := tui.New(tui.WithText("No maintenance actions recorded yet."))
	root.AddChild(empty)

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · q: Quit]"),
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

func (d *doctorApp) renderConfirmFix() *tui.Element {
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
		tui.WithBorderStyle(tui.NewStyle().Foreground(catppuccinYellow)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("Automatic Maintenance Notice"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow).Bold()),
	)
	card.AddChild(title)

	msg := tui.New(
		tui.WithText(legacyMaintenanceMessage),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinYellow)),
	)
	card.AddChild(msg)

	nav := tui.New(
		tui.WithText("[Press Esc or 'b' to return]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(nav)

	wrapper.AddChild(card)
	return wrapper
}

func (d *doctorApp) renderFixDone() *tui.Element {
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
		tui.WithText("Action Completed"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinGreen).Bold()),
	)
	card.AddChild(title)

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Foreground(catppuccinDim)),
	)
	card.AddChild(nav)

	wrapper.AddChild(card)
	return wrapper
}
