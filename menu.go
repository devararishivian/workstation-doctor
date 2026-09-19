// This file owns the interactive menu. Two variants exist because the
// modern chrome needs a real terminal: a huh select menu with a spinner
// on a TTY, and the classic numbered menu everywhere else (pipes,
// tests, automation).
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

// menuTitleStyle paints the menu header in Catppuccin blue. It renders
// only on a capable terminal; elsewhere lipgloss leaves text plain.
var menuTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89B4FA"))

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

func fancyMenu(ctx context.Context, cmd *cli.Command) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	out := newOutputLogger(cmd.Root().Writer)
	for {
		out.Info().Msg(menuTitleStyle.Render("workstation-doctor"))
		var choice string
		if err := huh.NewSelect[string]().
			Title("What do you want to do?").
			Description(lastRunSummary(cmd)).
			Options(
				huh.NewOption("Check status (read-only)", "check"),
				huh.NewOption("Show ordered manual steps", "manual"),
				huh.NewOption("Run automatic fix", "fix"),
				huh.NewOption("Show history", "history"),
				huh.NewOption("Quit", "quit"),
			).
			Value(&choice).
			WithTheme(huh.ThemeCatppuccin()).
			Run(); err != nil {
			return nil
		}
		switch choice {
		case "check":
			results, runID, err := gatherWithSpinner(ctx, cmd, "Running checks")
			if err != nil {
				menuRun(err)
				continue
			}
			menuRun(renderCheck(out, results, runID))
		case "manual":
			results, runID, err := gatherWithSpinner(ctx, cmd, "Running checks")
			if err != nil {
				menuRun(err)
				continue
			}
			menuRun(renderManual(out, results, runID))
		case "fix":
			results, runID, err := gatherWithSpinner(ctx, cmd, "Running checks")
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
		case "quit":
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
	runs, err := st.ListRuns(1)
	if err != nil || len(runs) == 0 {
		return "No runs recorded yet"
	}
	r := runs[0]
	return fmt.Sprintf("Last run #%d · %d OK · %d update · %d unknown",
		r.ID, r.NOk, r.NUpdate, r.NUnknown)
}

// spinModel is a minimal Bubble Tea program: a dot spinner beside a
// title. It runs until the caller quits it after the work finishes.
type spinModel struct {
	spinner spinner.Model
	title   string
}

func newSpinModel(title string) spinModel {
	return spinModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		title:   title,
	}
}

func (m spinModel) Init() tea.Cmd { return m.spinner.Tick }

func (m spinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m spinModel) View() string {
	return " " + m.spinner.View() + " " + m.title
}

// gatherWithSpinner runs the checks while a spinner animates. The work
// runs in the background and the done channel proves the results are
// fully written before they are read.
func gatherWithSpinner(ctx context.Context, cmd *cli.Command, title string) ([]doctor.Result, int64, error) {
	var results []doctor.Result
	var runID int64
	var runErr error
	done := make(chan struct{})
	p := tea.NewProgram(newSpinModel(title))
	go func() {
		defer close(done)
		results, runID, runErr = runAndRecord(ctx, cmd)
		p.Quit()
	}()
	if _, err := p.Run(); err != nil {
		<-done
		return nil, 0, fmt.Errorf("menu spinner failed: %w", err)
	}
	<-done
	return results, runID, runErr
}
