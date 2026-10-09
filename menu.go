// Package main provides the interactive Go-TUI dashboard.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"
	"unicode/utf8"
	"workstation-doctor/internal/app"

	tui "github.com/grindlemire/go-tui"
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
	case "results":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.openSelectedComponentDetail() }),
			tui.On(tui.Rune('d'), func(_ tui.KeyEvent) { d.openSelectedComponentDetail() }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
			tui.On(tui.KeyDown, func(_ tui.KeyEvent) {
				d.selectedResult.Update(func(v int) int {
					rows := buildDashboardRows(d.report.Get())
					if v < len(rows)-1 {
						return v + 1
					}
					return v
				})
			}),
			tui.On(tui.Rune('j'), func(_ tui.KeyEvent) {
				d.selectedResult.Update(func(v int) int {
					rows := buildDashboardRows(d.report.Get())
					if v < len(rows)-1 {
						return v + 1
					}
					return v
				})
			}),
			tui.On(tui.KeyUp, func(_ tui.KeyEvent) {
				d.selectedResult.Update(func(v int) int {
					if v > 0 {
						return v - 1
					}
					return 0
				})
			}),
			tui.On(tui.Rune('k'), func(_ tui.KeyEvent) {
				d.selectedResult.Update(func(v int) int {
					if v > 0 {
						return v - 1
					}
					return 0
				})
			}),
		}
	case "manual", "fix_done":
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
	case "detail":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("results") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("results") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("results") }),
			tui.On(tui.Rune('p'), func(_ tui.KeyEvent) { d.prepareFirstAction() }),
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
	case "preview_action":
		return tui.KeyMap{
			tui.On(tui.Rune('y'), func(_ tui.KeyEvent) { d.applyConfirmedAction() }),
			tui.On(tui.Rune('Y'), func(_ tui.KeyEvent) { d.applyConfirmedAction() }),
			tui.On(tui.Rune('n'), func(_ tui.KeyEvent) { d.declinePreparedAction() }),
			tui.On(tui.Rune('N'), func(_ tui.KeyEvent) { d.declinePreparedAction() }),
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.declinePreparedAction() }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.declinePreparedAction() }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.declinePreparedAction() }),
		}
	case "action_done":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("results") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("results") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("results") }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
		}
	case "history":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('m'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.openSelectedHistoryDetail() }),
			tui.On(tui.Rune('d'), func(_ tui.KeyEvent) { d.openSelectedHistoryDetail() }),
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
	case "history_detail":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("history") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("history") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("history") }),
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
	case "preview_migration":
		return tui.KeyMap{
			tui.On(tui.Rune('y'), func(_ tui.KeyEvent) { d.applyMigration(true) }),
			tui.On(tui.Rune('Y'), func(_ tui.KeyEvent) { d.applyMigration(true) }),
			tui.On(tui.Rune('n'), func(_ tui.KeyEvent) { d.applyMigration(false) }),
			tui.On(tui.Rune('N'), func(_ tui.KeyEvent) { d.applyMigration(false) }),
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.applyMigration(false) }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.applyMigration(false) }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.applyMigration(false) }),
		}
	case "migration_done":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('q'), func(_ tui.KeyEvent) { d.stop() }),
		}
	case "no_fixes":
		return tui.KeyMap{
			tui.On(tui.KeyEscape, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.KeyEnter, func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('b'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('m'), func(_ tui.KeyEvent) { d.mode.Set("menu") }),
			tui.On(tui.Rune('2'), func(_ tui.KeyEvent) { d.runAction("manual") }),
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
	switch key {
	case "check":
		d.startAudit()
	case "manual":
		rep := d.report.Get()
		if len(rep.Findings) == 0 {
			d.mode.Set("running")
			d.statusMsg.Set("Running audit to collect manual steps...")
			go func() {
				if d.service != nil {
					rep = d.service.Audit(d.ctx)
				}
				d.queueUpdate(func() {
					d.report.Set(rep)
					d.scrollOffset.Set(0)
					d.mode.Set("manual")
				})
			}()
			return
		}
		d.scrollOffset.Set(0)
		d.mode.Set("manual")
	case "fix":
		rep := d.report.Get()
		if len(rep.Findings) == 0 {
			d.mode.Set("running")
			d.statusMsg.Set("Running audit to find pending fixes...")
			go func() {
				if d.service != nil {
					rep = d.service.Audit(d.ctx)
				}
				d.queueUpdate(func() {
					d.report.Set(rep)
					d.triggerAutomaticFix()
				})
			}()
			return
		}
		d.triggerAutomaticFix()
	case "history":
		d.loadHistory()
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
