// Package main provides the interactive menu. Two variants exist:
// a modern declarative Go-TUI menu on a real terminal (TTY),
// and a classic numbered menu everywhere else (pipes, tests, automation).
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

// isInteractive reports whether both input and output are terminals.
// Fancy chrome requires both: without a TTY on stdin no keys arrive,
// and without a TTY on stdout escape codes would pollute a pipe.
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

type menuApp struct {
	summary  string
	selected *tui.State[int]
	choice   *tui.State[string]
	items    []menuItem
}

func newMenuApp(summary string) *menuApp {
	return &menuApp{
		summary:  summary,
		selected: tui.NewState(0),
		choice:   tui.NewState(""),
		items: []menuItem{
			{key: "check", shortcut: "1", label: "Check status (read-only)"},
			{key: "manual", shortcut: "2", label: "Show ordered manual steps"},
			{key: "fix", shortcut: "3", label: "Run automatic fix"},
			{key: "history", shortcut: "4", label: "Show history"},
			{key: "quit", shortcut: "0", label: "Quit"},
		},
	}
}

func (m *menuApp) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.On(tui.KeyDown, func(_ tui.KeyEvent) { m.selectNext() }),
		tui.On(tui.Rune('j'), func(_ tui.KeyEvent) { m.selectNext() }),
		tui.On(tui.KeyUp, func(_ tui.KeyEvent) { m.selectPrev() }),
		tui.On(tui.Rune('k'), func(_ tui.KeyEvent) { m.selectPrev() }),
		tui.On(tui.KeyEnter, func(ke tui.KeyEvent) {
			idx := m.selected.Get()
			if idx >= 0 && idx < len(m.items) {
				m.choice.Set(m.items[idx].key)
			}
			ke.App().Stop()
		}),
		tui.On(tui.Rune('1'), func(ke tui.KeyEvent) { m.choose("check", ke.App()) }),
		tui.On(tui.Rune('2'), func(ke tui.KeyEvent) { m.choose("manual", ke.App()) }),
		tui.On(tui.Rune('3'), func(ke tui.KeyEvent) { m.choose("fix", ke.App()) }),
		tui.On(tui.Rune('4'), func(ke tui.KeyEvent) { m.choose("history", ke.App()) }),
		tui.On(tui.Rune('0'), func(ke tui.KeyEvent) { m.choose("quit", ke.App()) }),
		tui.On(tui.Rune('q'), func(ke tui.KeyEvent) { m.choose("quit", ke.App()) }),
		tui.On(tui.KeyEscape, func(ke tui.KeyEvent) { m.choose("quit", ke.App()) }),
	}
}

func (m *menuApp) selectNext() {
	m.selected.Update(func(v int) int {
		if v >= len(m.items)-1 {
			return 0
		}
		return v + 1
	})
}

func (m *menuApp) selectPrev() {
	m.selected.Update(func(v int) int {
		if v <= 0 {
			return len(m.items) - 1
		}
		return v - 1
	})
}

func (m *menuApp) choose(key string, app *tui.App) {
	m.choice.Set(key)
	if app != nil {
		app.Stop()
	}
}

func (m *menuApp) Render(_ *tui.App) *tui.Element {
	card := tui.New(
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
	card.AddChild(title)

	if m.summary != "" {
		sub := tui.New(
			tui.WithText(m.summary),
			tui.WithTextStyle(tui.NewStyle().Dim()),
		)
		card.AddChild(sub)
	}

	menuList := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithGap(0),
	)

	currentSel := m.selected.Get()
	for i, item := range m.items {
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
		menuList.AddChild(line)
	}
	card.AddChild(menuList)

	hint := tui.New(
		tui.WithText("Use ↑/↓ or j/k to navigate · Enter to select · q to quit"),
		tui.WithTextStyle(tui.NewStyle().Dim()),
	)
	card.AddChild(hint)

	return card
}

func fancyMenu(ctx context.Context, cmd *cli.Command) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	out := newOutputLogger(cmd.Root().Writer)

	for {
		summary := lastRunSummary(cmd)
		menu := newMenuApp(summary)
		app, err := tui.NewApp(tui.WithRootComponent(menu))
		if err != nil {
			return fmt.Errorf("initialize tui app: %w", err)
		}
		if err := app.Run(); err != nil {
			_ = app.Close()
			return nil
		}
		_ = app.Close()

		choice := menu.choice.Get()
		switch choice {
		case "check":
			results, runID, err := gatherWithProgress(ctx, cmd, "Running checks")
			if err != nil {
				menuRun(err)
				continue
			}
			menuRun(renderCheck(out, results, runID))
		case "manual":
			results, runID, err := gatherWithProgress(ctx, cmd, "Running checks")
			if err != nil {
				menuRun(err)
				continue
			}
			menuRun(renderManual(out, results, runID))
		case "fix":
			results, runID, err := gatherWithProgress(ctx, cmd, "Running checks")
			if err != nil {
				menuRun(err)
				continue
			}
			path, err := dbPath(cmd)
			if err != nil {
				menuRun(err)
				continue
			}
			menuRun(runFixFlow(ctx, out, path, results, runID, false))
		case "history":
			menuRun(doHistory(ctx, cmd))
		case "quit", "":
			return nil
		}
	}
}

// lastRunSummary describes the newest recorded run for the menu
// subtitle. It is best effort: any storage problem yields a neutral
// line instead of an error, because the menu must always open.
func lastRunSummary(cmd *cli.Command) string {
	path, err := dbPath(cmd)
	if err != nil {
		return "History is unavailable"
	}
	st, err := store.Open(path)
	if err != nil {
		return "No runs recorded yet"
	}
	defer st.Close() //nolint:errcheck // close errors do not matter for a best-effort subtitle
	runs, err := st.ListRuns(context.Background(), 1)
	if err != nil || len(runs) == 0 {
		return "No runs recorded yet"
	}
	r := runs[0]
	return fmt.Sprintf("Last run #%d · %d OK · %d update · %d unknown",
		r.ID, r.NOk, r.NUpdate, r.NUnknown)
}

type checkOutput struct {
	results []doctor.Result
	runID   int64
	err     error
}

type progressApp struct {
	title  string
	dots   *tui.State[int]
	doneCh <-chan checkOutput
	output checkOutput
	app    *tui.App
}

func newProgressApp(title string, doneCh <-chan checkOutput) *progressApp {
	return &progressApp{
		title:  title,
		dots:   tui.NewState(0),
		doneCh: doneCh,
	}
}

func (p *progressApp) Watchers() []tui.Watcher {
	return []tui.Watcher{
		tui.OnTimer(200*time.Millisecond, func() {
			p.dots.Update(func(v int) int { return (v + 1) % 4 })
		}),
		tui.Watch(p.doneCh, func(out checkOutput) {
			p.output = out
			if p.app != nil {
				p.app.Stop()
			}
		}),
	}
}

func (p *progressApp) Render(app *tui.App) *tui.Element {
	p.app = app
	dotsStr := strings.Repeat(".", p.dots.Get())
	return tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
		tui.WithBorder(tui.BorderRounded),
		tui.WithBorderStyle(tui.NewStyle().Foreground(tui.Yellow)),
		tui.WithPadding(1),
		tui.WithAlign(tui.AlignCenter),
		tui.WithText(fmt.Sprintf("%s%s", p.title, dotsStr)),
		tui.WithTextStyle(tui.NewStyle().Foreground(tui.Yellow).Bold()),
	)
}

// gatherWithProgress runs checks while animating a progress box with Go-TUI.
func gatherWithProgress(ctx context.Context, cmd *cli.Command, title string) ([]doctor.Result, int64, error) {
	doneCh := make(chan checkOutput, 1)
	go func() {
		results, runID, err := runAndRecord(ctx, cmd)
		doneCh <- checkOutput{results: results, runID: runID, err: err}
	}()

	prog := newProgressApp(title, doneCh)
	app, err := tui.NewApp(tui.WithRootComponent(prog))
	if err != nil {
		out := <-doneCh
		return out.results, out.runID, out.err
	}
	_ = app.Run()
	_ = app.Close()

	return prog.output.results, prog.output.runID, prog.output.err
}
