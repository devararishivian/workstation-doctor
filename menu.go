// Package main provides the interactive menu.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"
	"unicode/utf8"
	"workstation-doctor/internal/app"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"
)

var (
	spinnerFrames    = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	catppuccinBlue   = tui.RGBColor(0x89, 0xB4, 0xFA) // #89B4FA
	catppuccinGreen  = tui.RGBColor(0xA6, 0xE3, 0xA1) // #A6E3A1
	catppuccinYellow = tui.RGBColor(0xF9, 0xE2, 0xAF) // #F9E2AF
	catppuccinRed    = tui.RGBColor(0xF3, 0x8B, 0xA8) // #F38BA8
	catppuccinMauve  = tui.RGBColor(0xCB, 0xA6, 0xF7) // #CBA6F7
	catppuccinDim    = tui.RGBColor(0x93, 0x99, 0xB2) // #9399B2
)

// isInteractive reports whether both input and output are terminals.
func isInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

func runMenu(ctx context.Context, cmd *cli.Command) error {
	if !isInteractive() {
		return classicMenu(ctx, cmd)
	}

	engine, err := doctor.NewAuditEngine(doctor.BuiltinDefinitions(), doctor.DefaultLimits())
	if err != nil {
		return fmt.Errorf("initialize engine: %w", err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		return fmt.Errorf("initialize host: %w", err)
	}
	path, _ := dbPath(cmd)
	stateDir := os.TempDir()
	if h, err := os.UserHomeDir(); err == nil {
		stateDir = h + "/.local/state/workstation-doctor"
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		Version:  version,
		DBPath:   path,
		StateDir: stateDir,
		Policy:   store.DefaultPolicy(),
	})

	return runTUI(ctx, service)
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
			tui.On(tui.Rune('0'), func(_ tui.KeyEvent) { d.stop() }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.stop() }),
		}
	case "results", "manual", "history", "fix_done":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('m'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
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
			tui.On(tui.Rune('n'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('N'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
		}
	default:
		return tui.KeyMap{
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.stop() }),
		}
	}
}

func (d *doctorApp) runAction(key string) {
	if key == "fix" {
		d.statusMsg.Set(legacyMaintenanceMessage)
		d.mode.Set("menu")
		return
	}
	switch key {
	case "check":
		d.startAudit()
	case "manual":
		d.mode.Set("manual")
	case "history":
		d.mode.Set("history")
	case "quit":
		d.stop()
	}
}

func runTUI(ctx context.Context, service *app.Service) error {
	ctx, stopSignal := signal.NotifyContext(ctx, os.Interrupt)
	defer stopSignal()

	appComponent := newDoctorApp(ctx, service)
	tuiApp, err := tui.NewApp(
		tui.WithRootComponent(appComponent),
	)
	if err != nil {
		return fmt.Errorf("initialize tui app: %w", err)
	}
	appComponent.BindApp(tuiApp)
	if err := tuiApp.Run(); err != nil {
		return fmt.Errorf("run tui app: %w", err)
	}
	return nil
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
