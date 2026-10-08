package main

import (
	"context"
	"sync"
	"time"
	"workstation-doctor/internal/app"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
)

type menuItem struct {
	key      string
	shortcut string
	label    string
}

type doctorApp struct {
	ctx    context.Context
	cancel context.CancelFunc

	service *app.Service
	app     *tui.App

	mu sync.Mutex

	// UI state
	mode         *tui.State[string]
	selectedMenu *tui.State[int]
	scrollOffset *tui.State[int]
	statusMsg    *tui.State[string]
	lastSummary  *tui.State[string]
	tickCount    *tui.State[int]

	// Redesign service state
	report            *tui.State[doctor.AuditReport]
	currentGeneration uint64
	auditCancel       context.CancelFunc

	// Legacy compatibility state
	results     *tui.State[[]doctor.Result]
	runID       *tui.State[int64]
	historyRuns *tui.State[[]store.Run]
	fixLogs     *tui.State[[]string]
	items       []menuItem
}

var (
	_ tui.AppBinder       = (*doctorApp)(nil)
	_ tui.KeyListener     = (*doctorApp)(nil)
	_ tui.Component       = (*doctorApp)(nil)
	_ tui.WatcherProvider = (*doctorApp)(nil)
)

func newDoctorApp(ctx context.Context, service *app.Service) *doctorApp {
	ctx, cancel := context.WithCancel(ctx)
	d := &doctorApp{
		ctx:          ctx,
		cancel:       cancel,
		service:      service,
		mode:         tui.NewState("menu"),
		selectedMenu: tui.NewState(0),
		scrollOffset: tui.NewState(0),
		statusMsg:    tui.NewState(""),
		lastSummary:  tui.NewState(""),
		tickCount:    tui.NewState(0),
		report:       tui.NewState(doctor.AuditReport{}),
		results:      tui.NewState([]doctor.Result{}),
		runID:        tui.NewState(int64(0)),
		historyRuns:  tui.NewState([]store.Run{}),
		fixLogs:      tui.NewState([]string{}),
		items: []menuItem{
			{key: "check", shortcut: "1", label: "Check status (read-only)"},
			{key: "manual", shortcut: "2", label: "Show ordered manual steps"},
			{key: "fix", shortcut: "3", label: "Run automatic fix"},
			{key: "history", shortcut: "4", label: "Show history"},
			{key: "quit", shortcut: "0", label: "Quit"},
		},
	}
	return d
}

func (d *doctorApp) BindApp(app *tui.App) {
	d.app = app
	d.mode.BindApp(app)
	d.selectedMenu.BindApp(app)
	d.scrollOffset.BindApp(app)
	d.statusMsg.BindApp(app)
	d.lastSummary.BindApp(app)
	d.tickCount.BindApp(app)
	d.report.BindApp(app)
	d.results.BindApp(app)
	d.runID.BindApp(app)
	d.historyRuns.BindApp(app)
	d.fixLogs.BindApp(app)
}

func (d *doctorApp) queueUpdate(fn func()) {
	if d.app != nil {
		d.app.QueueUpdate(fn)
	} else {
		fn()
	}
}

func (d *doctorApp) startAudit() {
	d.mu.Lock()
	d.currentGeneration++
	gen := d.currentGeneration

	if d.auditCancel != nil {
		d.auditCancel()
		d.auditCancel = nil
	}

	auditCtx, cancel := context.WithCancel(d.ctx)
	d.auditCancel = cancel
	d.mu.Unlock()

	d.mode.Set("running")
	d.statusMsg.Set("Running workstation audit checks...")

	go func() {
		var rep doctor.AuditReport
		if d.service != nil {
			rep = d.service.Audit(auditCtx)
		}

		d.queueUpdate(func() {
			d.mu.Lock()
			defer d.mu.Unlock()

			// Check generation to guard against late results
			if gen != d.currentGeneration || d.ctx.Err() != nil {
				return
			}

			d.report.Set(rep)
			d.scrollOffset.Set(0)
			d.mode.Set("results")
		})
	}()
}

func (d *doctorApp) stop() {
	d.mu.Lock()
	if d.auditCancel != nil {
		d.auditCancel()
		d.auditCancel = nil
	}
	d.cancel()
	d.mu.Unlock()

	if d.app != nil {
		d.app.Stop()
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

var _ = time.Millisecond
