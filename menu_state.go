package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/devararishivian/workstation-doctor/internal/app"
	"github.com/devararishivian/workstation-doctor/internal/doctor"
	"github.com/devararishivian/workstation-doctor/internal/store"

	tui "github.com/grindlemire/go-tui"
)

type doctorApp struct {
	ctx    context.Context
	cancel context.CancelFunc

	service *app.Service
	app     *tui.App

	mu sync.Mutex

	// UI state
	mode           *tui.State[string]
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
	preparedActions   []app.PreparedAction
	actionReport      *app.ActionReport
	batchReports      []app.ActionReport
	historyActions    *tui.State[[]store.ActionRecord]
	migrationPreview  *store.MigrationPreview
	migrationResult   *store.MigrationResult
	currentGeneration uint64
	auditCancel       context.CancelFunc
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
		mode:           tui.NewState("checking"),
		selectedResult: tui.NewState(0),
		scrollOffset:   tui.NewState(0),
		statusMsg:      tui.NewState(""),
		lastSummary:    tui.NewState(""),
		tickCount:      tui.NewState(0),
		report:         tui.NewState(doctor.AuditReport{}),
		activeDetail:   tui.NewState(""),
		currentDetail:  tui.NewState(Detail{}),
		historyActions: tui.NewState([]store.ActionRecord{}),
	}
	return d
}

func (d *doctorApp) BindApp(app *tui.App) {
	d.app = app
	if app != nil {
		d.mode.BindApp(app)
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
	go d.startAudit()
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

	d.mode.Set("checking")
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
	d.preparedActions = nil
	if d.mode.Get() == "preview_action" {
		d.mode.Set("results")
	}
}

func (d *doctorApp) applyConfirmedAction() {
	preps := d.preparedActions
	if len(preps) == 0 && d.preparedAction != nil {
		preps = []app.PreparedAction{*d.preparedAction}
	}
	if len(preps) == 0 || d.service == nil {
		d.declinePreparedAction()
		return
	}

	d.mode.Set("fixing")
	d.statusMsg.Set(fmt.Sprintf("Executing maintenance actions (0/%d completed)...", len(preps)))

	go func() {
		var reports []app.ActionReport
		for i, prep := range preps {
			d.queueUpdate(func() {
				d.statusMsg.Set(fmt.Sprintf("Executing maintenance action %d/%d: %s...", i+1, len(preps), prep.Preview().Label))
			})
			approval, err := d.service.Confirm(prep)
			if err != nil {
				reports = append(reports, app.ActionReport{
					Execution:    store.ExecutionFailed,
					Verification: store.VerificationNotPerformed,
					SafeError:    fmt.Sprintf("Confirmation failed for %s: %v", prep.Preview().Label, err),
				})
				continue
			}
			rep, applyErr := d.service.Apply(d.ctx, prep, approval)
			if applyErr != nil && rep.RecordID == "" {
				rep = app.ActionReport{
					Execution:    store.ExecutionFailed,
					Verification: store.VerificationNotPerformed,
					SafeError:    fmt.Sprintf("%s: %v", prep.Preview().Label, applyErr),
				}
			}
			reports = append(reports, rep)
		}

		d.queueUpdate(func() {
			d.preparedAction = nil
			d.preparedActions = nil
			d.statusMsg.Set("")
			d.batchReports = reports
			if len(reports) == 1 {
				d.actionReport = &reports[0]
				d.activeDetail.Set(actionReportText(reports[0]))
			} else {
				d.actionReport = nil
				d.activeDetail.Set("")
			}
			d.scrollOffset.Set(0)
			d.mode.Set("action_done")
		})
	}()
}

func (d *doctorApp) returnFromActionDone() {
	d.preparedAction = nil
	d.preparedActions = nil
	d.actionReport = nil
	d.batchReports = nil
	d.statusMsg.Set("")
	d.startAudit()
}

func (d *doctorApp) prepareAutomaticFixes(filterComponent string) {
	rep := d.report.Get()
	var preps []app.PreparedAction
	seenCmd := map[string]bool{}

	for _, f := range rep.Findings {
		if filterComponent != "" && f.Key.CheckID != filterComponent {
			continue
		}
		for _, a := range f.Actions {
			if a.Mode == doctor.ActionAutomatic {
				cmd := ""
				if len(a.Steps) > 0 && len(a.Steps[0].Command.Args) > 0 {
					cmd = a.Steps[0].Command.Executable + " " + strings.Join(a.Steps[0].Command.Args, " ")
				}
				if cmd != "" && seenCmd[cmd] {
					continue
				}
				if d.service == nil {
					d.statusMsg.Set("Service unavailable")
					return
				}
				prep, err := d.service.Prepare(d.ctx, f.Key, a.ID)
				if err != nil {
					continue
				}
				if cmd != "" {
					seenCmd[cmd] = true
				}
				preps = append(preps, prep)
			}
		}
	}

	if len(preps) == 0 {
		if filterComponent != "" {
			d.statusMsg.Set("No automatic maintenance action available for " + filterComponent)
		} else {
			d.statusMsg.Set("No actions needed. All components are current.")
		}
		d.mode.Set("no_fixes")
		return
	}

	d.preparedActions = preps
	if len(preps) == 1 {
		d.preparedAction = &preps[0]
		det := actionDetail(preps[0].Preview())
		d.currentDetail.Set(det)
		d.activeDetail.Set(detailText(det))
	} else {
		d.preparedAction = nil
		d.currentDetail.Set(Detail{})
		d.activeDetail.Set("")
	}
	d.scrollOffset.Set(0)
	d.mode.Set("preview_action")
}

func (d *doctorApp) prepareFirstAction() {
	rep := d.report.Get()
	rows := buildDashboardRows(rep)
	if len(rows) == 0 {
		return
	}
	sel := d.selectedResult.Get()
	if sel < 0 || sel >= len(rows) {
		sel = 0
	}
	comp := rows[sel].Component
	d.prepareAutomaticFixes(comp)
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
	d.selectedResult.Set(0)
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
		d.mode.Set("results")
		return
	}
	prev := *d.migrationPreview
	res, err := d.service.MigrateLegacy(d.ctx, prev, true)
	if err != nil {
		d.statusMsg.Set("Migration failed: " + err.Error())
		d.migrationPreview = nil
		res = store.MigrationResult{
			Imported:        0,
			PreservedSource: prev.Source,
		}
		d.migrationResult = &res
		d.mode.Set("migration_done")
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
	idx := d.selectedResult.Get()
	if idx < 0 || idx >= len(actions) {
		idx = 0
	}
	_ = d.showHistoryDetail(actions[idx])
}

func (d *doctorApp) triggerAutomaticFix() {
	d.prepareAutomaticFixes("")
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

var _ = time.Millisecond
