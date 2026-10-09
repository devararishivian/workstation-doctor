package main

import (
	"context"
	"sync"
	"time"

	"github.com/devararishivian/workstation-doctor/internal/app"
	"github.com/devararishivian/workstation-doctor/internal/doctor"
	"github.com/devararishivian/workstation-doctor/internal/store"

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
	mode           *tui.State[string]
	selectedMenu   *tui.State[int]
	selectedResult *tui.State[int]
	scrollOffset   *tui.State[int]
	statusMsg      *tui.State[string]
	lastSummary    *tui.State[string]
	tickCount      *tui.State[int]

	// Redesign service state
	report            *tui.State[doctor.AuditReport]
	activeDetail      *tui.State[string]
	currentDetail     *tui.State[Detail]
	preparedAction    *app.PreparedAction
	actionReport      *app.ActionReport
	historyActions    *tui.State[[]store.ActionRecord]
	migrationPreview  *store.MigrationPreview
	migrationResult   *store.MigrationResult
	currentGeneration uint64
	auditCancel       context.CancelFunc

	items []menuItem
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
		ctx:            ctx,
		cancel:         cancel,
		service:        service,
		mode:           tui.NewState("menu"),
		selectedMenu:   tui.NewState(0),
		selectedResult: tui.NewState(0),
		scrollOffset:   tui.NewState(0),
		statusMsg:      tui.NewState(""),
		lastSummary:    tui.NewState(""),
		tickCount:      tui.NewState(0),
		report:         tui.NewState(doctor.AuditReport{}),
		activeDetail:   tui.NewState(""),
		currentDetail:  tui.NewState(Detail{}),
		historyActions: tui.NewState([]store.ActionRecord{}),
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
	d.selectedResult.BindApp(app)
	d.scrollOffset.BindApp(app)
	d.statusMsg.BindApp(app)
	d.lastSummary.BindApp(app)
	d.tickCount.BindApp(app)
	d.report.BindApp(app)
	d.activeDetail.BindApp(app)
	d.currentDetail.BindApp(app)
	d.historyActions.BindApp(app)
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
			d.statusMsg.Set("")
			d.mode.Set("results")
		})
	}()
}

func (d *doctorApp) openSelectedComponentDetail() {
	rep := d.report.Get()
	rows := buildDashboardRows(rep)
	if len(rows) == 0 {
		return
	}
	sel := d.selectedResult.Get()
	if sel < 0 || sel >= len(rows) {
		sel = 0
	}
	row := rows[sel]
	det := componentDetail(rep, row.Component)
	d.currentDetail.Set(det)
	d.activeDetail.Set(detailText(det))
	d.scrollOffset.Set(0)
	d.mode.Set("detail")
}

func (d *doctorApp) prepareAction(key doctor.FindingKey, actionID string) {
	if d.service == nil {
		d.statusMsg.Set("Service unavailable")
		return
	}
	prep, err := d.service.Prepare(d.ctx, key, actionID)
	if err != nil {
		d.statusMsg.Set("Prepare failed: " + err.Error())
		return
	}
	d.preparedAction = &prep
	det := actionDetail(prep.Preview())
	d.currentDetail.Set(det)
	d.activeDetail.Set(detailText(det))
	d.scrollOffset.Set(0)
	d.mode.Set("preview_action")
}

func (d *doctorApp) declinePreparedAction() {
	d.preparedAction = nil
	if d.mode.Get() == "preview_action" {
		d.mode.Set("results")
	}
}

func (d *doctorApp) applyConfirmedAction() {
	if d.preparedAction == nil || d.service == nil {
		d.declinePreparedAction()
		return
	}
	prep := *d.preparedAction
	approval, err := d.service.Confirm(prep)
	if err != nil {
		d.statusMsg.Set("Confirm failed: " + err.Error())
		d.declinePreparedAction()
		return
	}

	d.mode.Set("fixing")
	d.statusMsg.Set("Executing confirmed maintenance action...")

	go func() {
		rep, applyErr := d.service.Apply(d.ctx, prep, approval)
		d.queueUpdate(func() {
			d.preparedAction = nil
			d.statusMsg.Set("")
			if applyErr != nil && rep.RecordID == "" {
				d.statusMsg.Set("Action failed: " + applyErr.Error())
				d.mode.Set("results")
				return
			}
			d.actionReport = &rep
			d.activeDetail.Set(actionReportText(rep))
			d.scrollOffset.Set(0)
			d.mode.Set("action_done")
		})
	}()
}

func (d *doctorApp) returnFromActionDone() {
	d.statusMsg.Set("")
	d.startAudit()
}

func (d *doctorApp) prepareFirstAction() {
	rep := d.report.Get()
	idx := d.scrollOffset.Get()
	if idx < 0 || idx >= len(rep.Findings) {
		idx = 0
	}
	if len(rep.Findings) == 0 {
		return
	}
	f := rep.Findings[idx]
	for _, a := range f.Actions {
		if a.Mode == doctor.ActionAutomatic {
			d.prepareAction(f.Key, a.ID)
			return
		}
	}
	d.statusMsg.Set("No automatic maintenance action available for this finding")
}

func (d *doctorApp) loadHistory() {
	if d.service == nil {
		d.statusMsg.Set("Service unavailable")
		return
	}
	page, err := d.service.History(d.ctx, store.ActionQuery{Limit: 50})
	if err != nil {
		d.statusMsg.Set("Failed to load history: " + err.Error())
		return
	}
	d.historyActions.Set(page.Items)
	d.scrollOffset.Set(0)
	d.mode.Set("history")
}

func (d *doctorApp) showHistoryDetail(record store.ActionRecord) error {
	detail := historyDetail(record)
	d.currentDetail.Set(detail)
	d.activeDetail.Set(detailText(detail))
	d.scrollOffset.Set(0)
	d.mode.Set("history_detail")
	return nil
}

func (d *doctorApp) previewMigration(source, destination string) {
	if d.service == nil {
		d.statusMsg.Set("Service unavailable")
		return
	}
	prev, err := d.service.PreviewMigration(d.ctx, source, destination)
	if err != nil {
		d.statusMsg.Set("Preview migration failed: " + err.Error())
		return
	}
	d.migrationPreview = &prev
	d.scrollOffset.Set(0)
	d.mode.Set("preview_migration")
}

func (d *doctorApp) applyMigration(confirmed bool) {
	if !confirmed || d.migrationPreview == nil || d.service == nil {
		d.migrationPreview = nil
		d.mode.Set("menu")
		return
	}
	prev := *d.migrationPreview
	res, err := d.service.MigrateLegacy(d.ctx, prev, true)
	if err != nil {
		d.statusMsg.Set("Migration failed: " + err.Error())
		d.migrationPreview = nil
		d.mode.Set("menu")
		return
	}
	d.migrationPreview = nil
	d.migrationResult = &res
	d.scrollOffset.Set(0)
	d.mode.Set("migration_done")
}

func (d *doctorApp) openSelectedHistoryDetail() {
	actions := d.historyActions.Get()
	if len(actions) == 0 {
		return
	}
	idx := d.scrollOffset.Get()
	if idx < 0 || idx >= len(actions) {
		idx = 0
	}
	_ = d.showHistoryDetail(actions[idx])
}

func (d *doctorApp) triggerAutomaticFix() {
	rep := d.report.Get()
	for _, f := range rep.Findings {
		for _, a := range f.Actions {
			if a.Mode == doctor.ActionAutomatic {
				d.prepareAction(f.Key, a.ID)
				return
			}
		}
	}
	d.statusMsg.Set("No actions needed. All components are current.")
	d.mode.Set("no_fixes")
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
