// Package main provides the interactive menu. Two variants exist:
// a modern declarative Go-TUI dashboard on a real terminal (TTY),
// and a classic numbered menu everywhere else (pipes, tests, automation).
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
	"unicode/utf8"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// isInteractive reports whether both input and output are terminals.
func isInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

func runMenu(ctx context.Context, cmd *cli.Command) error {
	if !isInteractive() {
		return classicMenu(ctx, cmd)
	}
	return fancyMenu(ctx, cmd)
}

func classicMenu(ctx context.Context, cmd *cli.Command) error {
	out := newOutputLogger(cmd.Root().Writer)
	in := bufio.NewScanner(os.Stdin)
	for {
		out.Info().Msg("\nworkstation-doctor")
		out.Info().Msg("  1. Check status (read-only)")
		out.Info().Msg("  2. Show ordered manual steps")
		out.Info().Msg("  3. Run automatic fix")
		out.Info().Msg("  4. Show history")
		out.Info().Msg("  0. Quit")
		out.Info().Msg("Select [0-4]: ")
		if !in.Scan() {
			return nil
		}
		switch strings.TrimSpace(in.Text()) {
		case "1":
			menuRun(doCheck(ctx, cmd))
		case "2":
			menuRun(doManual(ctx, cmd))
		case "3":
			menuRun(doFix(ctx, cmd, false))
		case "4":
			menuRun(doHistory(ctx, cmd))
		case "0", "q", "quit", "exit":
			return nil
		default:
			out.Info().Msg("Unknown selection.")
		}
	}
}

type menuItem struct {
	key      string
	shortcut string
	label    string
}

type doctorApp struct {
	ctx          context.Context
	cmd          *cli.Command
	app          *tui.App
	mode         *tui.State[string]
	selectedMenu *tui.State[int]
	scrollOffset *tui.State[int]
	results      *tui.State[[]doctor.Result]
	runID        *tui.State[int64]
	historyRuns  *tui.State[[]store.Run]
	statusMsg    *tui.State[string]
	lastSummary  *tui.State[string]
	tickCount    *tui.State[int]
	fixLogs      *tui.State[[]string]
	items        []menuItem
}

var (
	_ tui.AppBinder       = (*doctorApp)(nil)
	_ tui.KeyListener     = (*doctorApp)(nil)
	_ tui.Component       = (*doctorApp)(nil)
	_ tui.WatcherProvider = (*doctorApp)(nil)
)

func newDoctorApp(ctx context.Context, cmd *cli.Command) *doctorApp {
	return &doctorApp{
		ctx:          ctx,
		cmd:          cmd,
		mode:         tui.NewState("menu"),
		selectedMenu: tui.NewState(0),
		scrollOffset: tui.NewState(0),
		results:      tui.NewState([]doctor.Result{}),
		runID:        tui.NewState(int64(0)),
		historyRuns:  tui.NewState([]store.Run{}),
		statusMsg:    tui.NewState(""),
		lastSummary:  tui.NewState(lastRunSummary(cmd)),
		tickCount:    tui.NewState(0),
		fixLogs:      tui.NewState([]string{}),
		items: []menuItem{
			{key: "check", shortcut: "1", label: "Check status (read-only)"},
			{key: "manual", shortcut: "2", label: "Show ordered manual steps"},
			{key: "fix", shortcut: "3", label: "Run automatic fix"},
			{key: "history", shortcut: "4", label: "Show history"},
			{key: "quit", shortcut: "0", label: "Quit"},
		},
	}
}

func (d *doctorApp) BindApp(app *tui.App) {
	d.app = app
	d.mode.BindApp(app)
	d.selectedMenu.BindApp(app)
	d.scrollOffset.BindApp(app)
	d.results.BindApp(app)
	d.runID.BindApp(app)
	d.historyRuns.BindApp(app)
	d.statusMsg.BindApp(app)
	d.lastSummary.BindApp(app)
	d.tickCount.BindApp(app)
	d.fixLogs.BindApp(app)
}

func (d *doctorApp) Watchers() []tui.Watcher {
	return []tui.Watcher{
		tui.OnTimer(120*time.Millisecond, func() {
			currentMode := d.mode.Get()
			if currentMode == "running" || currentMode == "fixing" {
				d.tickCount.Update(func(v int) int { return (v + 1) % len(spinnerFrames) })
			}
		}),
	}
}

func (d *doctorApp) KeyMap() tui.KeyMap {
	switch d.mode.Get() {
	case "menu":
		return tui.KeyMap{
			tui.On(tui.KeyDown, func(_ tui.KeyEvent) { d.selectNext() }),
			tui.On(tui.Rune('j'), func(_ tui.KeyEvent) { d.selectNext() }),
			tui.On(tui.KeyUp, func(_ tui.KeyEvent) { d.selectPrev() }),
			tui.On(tui.Rune('k'), func(_ tui.KeyEvent) { d.selectPrev() }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.activateSelected() }),
			tui.On(tui.Rune('1'), func(_ tui.KeyEvent) { d.runAction("check") }),
			tui.On(tui.Rune('2'), func(_ tui.KeyEvent) { d.runAction("manual") }),
			tui.On(tui.Rune('3'), func(_ tui.KeyEvent) { d.runAction("fix") }),
			tui.On(tui.Rune('4'), func(_ tui.KeyEvent) { d.runAction("history") }),
			tui.On(tui.Rune('0'), func(ke tui.KeyEvent) { ke.App().Stop() }),
			tui.On(tui.Rune('q'), func(ke tui.KeyEvent) { ke.App().Stop() }),
			tui.On(tui.KeyEscape, func(ke tui.KeyEvent) { ke.App().Stop() }),
		}
	case "results", "manual", "history", "fix_done":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('m'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('q'), func(ke tui.KeyEvent) { ke.App().Stop() }),
			tui.On(tui.KeyDown, func(_ tui.KeyEvent) { d.scrollOffset.Update(func(v int) int { return v + 1 }) }),
			tui.On(tui.Rune('j'), func(_ tui.KeyEvent) { d.scrollOffset.Update(func(v int) int { return v + 1 }) }),
			tui.On(tui.KeyUp, func(_ tui.KeyEvent) {
				d.scrollOffset.Update(func(v int) int {
					if v > 0 {
						return v - 1
					}
					return 0
				})
			}),
			tui.On(tui.Rune('k'), func(_ tui.KeyEvent) {
				d.scrollOffset.Update(func(v int) int {
					if v > 0 {
						return v - 1
					}
					return 0
				})
			}),
		}
	case "confirm_fix":
		return tui.KeyMap{
			tui.On(tui.Rune('y'), func(_ tui.KeyEvent) { d.executeFix() }),
			tui.On(tui.Rune('Y'), func(_ tui.KeyEvent) { d.executeFix() }),
			tui.On(tui.Rune('n'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('N'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('q'), func(ke tui.KeyEvent) { ke.App().Stop() }),
		}
	default:
		return tui.KeyMap{
			tui.On(tui.Rune('q'), func(ke tui.KeyEvent) { ke.App().Stop() }),
			tui.On(tui.KeyEscape, func(ke tui.KeyEvent) { ke.App().Stop() }),
		}
	}
}

func (d *doctorApp) selectNext() {
	d.selectedMenu.Update(func(v int) int {
		if v >= len(d.items)-1 {
			return 0
		}
		return v + 1
	})
}

func (d *doctorApp) selectPrev() {
	d.selectedMenu.Update(func(v int) int {
		if v <= 0 {
			return len(d.items) - 1
		}
		return v - 1
	})
}

func (d *doctorApp) activateSelected() {
	idx := d.selectedMenu.Get()
	if idx >= 0 && idx < len(d.items) {
		d.runAction(d.items[idx].key)
	}
}

func (d *doctorApp) runAction(key string) {
	switch key {
	case "check":
		d.mode.Set("running")
		d.statusMsg.Set("Running workstation audit checks...")
		go func() {
			results, runID, err := runAndRecord(d.ctx, d.cmd)
			if d.app == nil {
				return
			}
			d.app.QueueUpdate(func() {
				if err != nil {
					d.statusMsg.Set("Audit failed: " + err.Error())
					d.mode.Set("menu")
					return
				}
				d.results.Set(results)
				d.runID.Set(runID)
				d.scrollOffset.Set(0)
				d.lastSummary.Set(lastRunSummary(d.cmd))
				d.mode.Set("results")
			})
		}()
	case "manual":
		d.mode.Set("running")
		d.statusMsg.Set("Running checks to gather manual steps...")
		go func() {
			results, runID, err := runAndRecord(d.ctx, d.cmd)
			if d.app == nil {
				return
			}
			d.app.QueueUpdate(func() {
				if err != nil {
					d.statusMsg.Set("Audit failed: " + err.Error())
					d.mode.Set("menu")
					return
				}
				d.results.Set(results)
				d.runID.Set(runID)
				d.scrollOffset.Set(0)
				d.lastSummary.Set(lastRunSummary(d.cmd))
				d.mode.Set("manual")
			})
		}()
	case "fix":
		d.mode.Set("running")
		d.statusMsg.Set("Running checks to find pending fixes...")
		go func() {
			results, runID, err := runAndRecord(d.ctx, d.cmd)
			if d.app == nil {
				return
			}
			d.app.QueueUpdate(func() {
				if err != nil {
					d.statusMsg.Set("Audit failed: " + err.Error())
					d.mode.Set("menu")
					return
				}
				d.results.Set(results)
				d.runID.Set(runID)
				d.scrollOffset.Set(0)
				d.lastSummary.Set(lastRunSummary(d.cmd))
				pending := doctor.Pending(results)
				if len(pending) == 0 {
					d.statusMsg.Set("No actions needed. All components are current.")
					d.mode.Set("results")
					return
				}
				d.mode.Set("confirm_fix")
			})
		}()
	case "history":
		path, err := dbPath(d.cmd)
		if err != nil {
			d.statusMsg.Set("Database error: " + err.Error())
			return
		}
		st := openStore(path)
		if st == nil {
			d.statusMsg.Set("History database failed to open")
			return
		}
		defer st.Close() //nolint:errcheck // close errors need no action
		runs, err := st.ListRuns(d.ctx, 20)
		if err != nil {
			d.statusMsg.Set("Failed to read history: " + err.Error())
			return
		}
		d.historyRuns.Set(runs)
		d.scrollOffset.Set(0)
		d.mode.Set("history")
	case "quit":
		if d.app != nil {
			d.app.Stop()
		}
	}
}

func (d *doctorApp) executeFix() {
	d.mode.Set("fixing")
	d.statusMsg.Set("Executing automatic fix commands...")
	results := d.results.Get()
	runID := d.runID.Get()
	pending := doctor.Pending(results)

	go func() {
		path, err := dbPath(d.cmd)
		var st *store.Store
		if err == nil {
			st = openStore(path)
			if st != nil {
				defer st.Close() //nolint:errcheck // close errors need no action
			}
		}

		logs := make([]string, 0, len(pending))
		fail := 0
		for _, r := range pending {
			start := time.Now()
			c, cancel := context.WithTimeout(d.ctx, 10*time.Minute)
			raw, cmdErr := exec.CommandContext(c, "/bin/sh", "-c", r.Fix).CombinedOutput()
			cancel()
			end := time.Now()
			status := "ok"
			if cmdErr != nil {
				status = "fail"
				fail++
				logs = append(logs, fmt.Sprintf("FAILED: %s", r.Fix))
			} else {
				logs = append(logs, fmt.Sprintf("OK: %s", r.Fix))
			}
			if st != nil {
				_ = st.RecordAction(d.ctx, runID, "fix", r.Fix, status, lastBytes(string(raw), 4096), start, end)
			}
		}
		if d.app != nil {
			d.app.QueueUpdate(func() {
				d.fixLogs.Set(logs)
				d.lastSummary.Set(lastRunSummary(d.cmd))
				d.mode.Set("fix_done")
			})
		}
	}()
}

func (d *doctorApp) Render(_ *tui.App) *tui.Element {
	switch d.mode.Get() {
	case "running", "fixing":
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
	default:
		return d.renderMenu()
	}
}

func (d *doctorApp) renderMenu() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Cyan)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("workstation-doctor"),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Cyan).Bold()),
	)
	root.AddChild(title)

	if summary := d.lastSummary.Get(); summary != "" {
		sub := tui.New(
			tui.WithText(summary),
			tui.WithTextStyle(tui.NewStyle().Dim()),
		)
		root.AddChild(sub)
	}

	if msg := d.statusMsg.Get(); msg != "" {
		st := tui.New(
			tui.WithText(msg),
			tui.WithTextStyle(tui.NewStyle().Foreground(tui.Yellow)),
		)
		root.AddChild(st)
	}

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
			prefix = " > "
			style = tui.NewStyle().Foreground(tui.Green).Bold()
		}

		line := tui.New(
			tui.WithText(fmt.Sprintf("%s[%s] %s", prefix, item.shortcut, item.label)),
			tui.WithTextStyle(style),
		)
		list.AddChild(line)
	}
	root.AddChild(list)

	hint := tui.New(
		tui.WithText("Use ↑/↓ or j/k to navigate · Enter to select · q to quit"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(hint)

	return root
}

func (d *doctorApp) renderRunning() *tui.Element {
	frame := spinnerFrames[d.tickCount.Get()%len(spinnerFrames)]
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Yellow)),
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
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Yellow).Bold()),
	)
	root.AddChild(spinText)

	hint := tui.New(
		tui.WithText("All audit checks are running concurrently in background..."),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(hint)

	return root
}

func (d *doctorApp) renderResults() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Cyan)),
		tui.WithPadding(1),
		tui.WithGap(0),
	)

	headerText := "Audit Results"
	if id := d.runID.Get(); id > 0 {
		headerText += fmt.Sprintf(" · Saved run #%d", id)
	}
	title := tui.New(
		tui.WithText(headerText),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Cyan).Bold()),
	)
	root.AddChild(title)

	cols := tui.New(
		tui.WithText(fmt.Sprintf("%-22s %-16s %-16s %-10s %s", "COMPONENT", "INSTALLED", "LATEST", "STATUS", "NOTE")),
		tui.WithTextStyle(tui.NewStyle().Bold()),
	)
	root.AddChild(cols)

	sep := tui.New(
		tui.WithText(strings.Repeat("─", 88)),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(sep)

	resList := d.results.Get()
	offset := d.scrollOffset.Get()
	if offset > len(resList)-1 && len(resList) > 0 {
		offset = len(resList) - 1
	}

	visible := resList
	if offset > 0 && offset < len(resList) {
		visible = resList[offset:]
	}

	maxDisplay := 18
	if len(visible) > maxDisplay {
		visible = visible[:maxDisplay]
	}

	for _, r := range visible {
		statusStyle := tui.NewStyle()
		switch r.Status {
		case doctor.StatusOK:
			statusStyle = statusStyle.Foreground(tui.Green)
		case doctor.StatusUpdate:
			statusStyle = statusStyle.Foreground(tui.Yellow).Bold()
		default:
			statusStyle = statusStyle.Foreground(tui.Red).Bold()
		}

		line := tui.New(
			tui.WithText(fmt.Sprintf("%-22s %-16s %-16s %-10s %s",
				trunc(r.Component, 22), trunc(r.Installed, 16), trunc(r.Latest, 16), r.Status, r.Note)),
			tui.WithTextStyle(statusStyle),
		)
		root.AddChild(line)
	}

	root.AddChild(sep)

	s := doctor.Summarize(resList)
	summaryLine := tui.New(
		tui.WithText(fmt.Sprintf("Summary: %d OK · %d need update · %d unknown", s.OK, s.Update, s.Unknown)),
		tui.WithTextStyle(tui.NewStyle().Bold()),
	)
	root.AddChild(summaryLine)

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · ↑/↓: Scroll · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderManual() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Yellow)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("Ordered Manual Steps"),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Yellow).Bold()),
	)
	root.AddChild(title)

	pending := doctor.Pending(d.results.Get())
	if len(pending) == 0 {
		msg := tui.New(tui.WithText("No action is needed. All components are current."))
		root.AddChild(msg)
	} else {
		for i, r := range pending {
			step := tui.New(
				tui.WithText(fmt.Sprintf("%d. %s", i+1, r.Manual)),
				tui.WithTextStyle(tui.NewStyle().Bold()),
			)
			root.AddChild(step)
		}
	}

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderHistory() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Cyan)),
		tui.WithPadding(1),
		tui.WithGap(0),
	)

	title := tui.New(
		tui.WithText("Run History (embedded SQLite)"),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Cyan).Bold()),
	)
	root.AddChild(title)

	cols := tui.New(
		tui.WithText(fmt.Sprintf("%-6s %-20s %-5s %-8s %-8s %s", "RUN", "STARTED", "OK", "UPDATE", "UNKNOWN", "EXIT")),
		tui.WithTextStyle(tui.NewStyle().Bold()),
	)
	root.AddChild(cols)

	sep := tui.New(
		tui.WithText(strings.Repeat("─", 65)),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(sep)

	runs := d.historyRuns.Get()
	if len(runs) == 0 {
		empty := tui.New(tui.WithText("No history recorded yet."))
		root.AddChild(empty)
	} else {
		for _, r := range runs {
			line := tui.New(
				tui.WithText(fmt.Sprintf("#%-5d %-20s %-5d %-8d %-8d %d",
					r.ID, shortTS(r.StartedAt), r.NOk, r.NUpdate, r.NUnknown, r.ExitCode)),
			)
			root.AddChild(line)
		}
	}

	root.AddChild(sep)

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderConfirmFix() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Yellow)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("Automatic Fix Confirmation"),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Yellow).Bold()),
	)
	root.AddChild(title)

	pending := doctor.Pending(d.results.Get())
	for i, r := range pending {
		line := tui.New(tui.WithText(fmt.Sprintf("%d. %s", i+1, r.Fix)))
		root.AddChild(line)
	}

	prompt := tui.New(
		tui.WithText(fmt.Sprintf("Run the %d commands above in order? [y/N]", len(pending))),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Yellow).Bold()),
	)
	root.AddChild(prompt)

	nav := tui.New(
		tui.WithText("[Press 'y' to confirm execution · Press 'n' or Esc to cancel]"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(nav)

	return root
}

func (d *doctorApp) renderFixDone() *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Green)),
		tui.WithPadding(1),
		tui.WithGap(1),
	)

	title := tui.New(
		tui.WithText("Fix Execution Completed"),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Green).Bold()),
	)
	root.AddChild(title)

	logs := d.fixLogs.Get()
	for _, l := range logs {
		style := tui.NewStyle()
		if strings.HasPrefix(l, "OK:") {
			style = style.Foreground(tui.Green)
		} else {
			style = style.Foreground(tui.Red).Bold()
		}
		line := tui.New(tui.WithText(l), tui.WithTextStyle(style))
		root.AddChild(line)
	}

	nav := tui.New(
		tui.WithText("[Esc / Enter / b: Back to Menu · q: Quit]"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	root.AddChild(nav)

	return root
}

func fancyMenu(ctx context.Context, cmd *cli.Command) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	doctorApp := newDoctorApp(ctx, cmd)
	app, err := tui.NewApp(
		tui.WithRootComponent(doctorApp),
	)
	if err != nil {
		return fmt.Errorf("initialize tui app: %w", err)
	}
	defer app.Close() //nolint:errcheck // close errors need no action on exit

	if err := app.Run(); err != nil {
		return fmt.Errorf("run tui app: %w", err)
	}
	return nil
}

func lastRunSummary(cmd *cli.Command) string {
	path, err := dbPath(cmd)
	if err != nil {
		return "History is unavailable"
	}
	st, err := store.Open(path)
	if err != nil {
		return "No runs recorded yet"
	}
	defer st.Close() //nolint:errcheck // close errors need no action
	runs, err := st.ListRuns(context.Background(), 1)
	if err != nil || len(runs) == 0 {
		return "No runs recorded yet"
	}
	r := runs[0]
	return fmt.Sprintf("Last run #%d · %d OK · %d update · %d unknown",
		r.ID, r.NOk, r.NUpdate, r.NUnknown)
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
