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
	selectFixItems *tui.State[[]fixCandidate]

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

type fixCandidate struct {
	Component string
	Label     string
	ActionID  string
	Key       doctor.FindingKey
	Selected  bool
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
		selectFixItems: tui.NewState([]fixCandidate{}),
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
		d.selectFixItems.BindApp(app)
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

func (d *doctorApp) enterSelectFixes() {
	rep := d.report.Get()
	var candidates []fixCandidate
	seenCmd := map[string]bool{}

	for _, f := range rep.Findings {
		for _, a := range f.Actions {
			if a.Mode == doctor.ActionAutomatic {
				cmd := ""
				if len(a.Steps) > 0 && len(a.Steps[0].Command.Args) > 0 {
					cmd = a.Steps[0].Command.Executable + " " + strings.Join(a.Steps[0].Command.Args, " ")
				}
				if cmd != "" && seenCmd[cmd] {
					continue
				}
				if cmd != "" {
					seenCmd[cmd] = true
				}
				candidates = append(candidates, fixCandidate{
					Component: f.Key.CheckID,
					Label:     a.Label,
					ActionID:  a.ID,
					Key:       f.Key,
					Selected:  true,
				})
			}
		}
	}

	if len(candidates) == 0 {
		d.statusMsg.Set("No actions needed. All components are current.")
		d.mode.Set("no_fixes")
		return
	}

	d.selectFixItems.Set(candidates)
	d.selectedResult.Set(0)
	d.scrollOffset.Set(0)
	d.mode.Set("select_fixes")
}

func (d *doctorApp) toggleFixItem(idx int) {
	items := d.selectFixItems.Get()
	if idx < 0 || idx >= len(items) {
		return
	}
	updated := make([]fixCandidate, len(items))
	copy(updated, items)
	updated[idx].Selected = !updated[idx].Selected
	d.selectFixItems.Set(updated)
}

func (d *doctorApp) selectAllFixes() {
	items := d.selectFixItems.Get()
	updated := make([]fixCandidate, len(items))
	for i, it := range items {
		it.Selected = true
		updated[i] = it
	}
	d.selectFixItems.Set(updated)
}

func (d *doctorApp) deselectAllFixes() {
	items := d.selectFixItems.Get()
	updated := make([]fixCandidate, len(items))
	for i, it := range items {
		it.Selected = false
		updated[i] = it
	}
	d.selectFixItems.Set(updated)
}

func (d *doctorApp) confirmSelectedFixes() {
	var selectedComponents []string
	for _, item := range d.selectFixItems.Get() {
		if item.Selected {
			selectedComponents = append(selectedComponents, item.Component)
		}
	}
	if len(selectedComponents) == 0 {
		d.statusMsg.Set("No actions needed. All components are current.")
		d.mode.Set("no_fixes")
		return
	}
	d.prepareAutomaticFixes(selectedComponents)
}

func (d *doctorApp) prepareAutomaticFixes(filterComponents []string) {
	rep := d.report.Get()
	var preps []app.PreparedAction
	seenCmd := map[string]bool{}

	var allowed map[string]bool
	if filterComponents != nil {
		allowed = make(map[string]bool, len(filterComponents))
		for _, c := range filterComponents {
			allowed[c] = true
		}
	}

	for _, f := range rep.Findings {
		if allowed != nil && !allowed[f.Key.CheckID] {
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
		if len(filterComponents) == 1 {
			d.statusMsg.Set("No automatic maintenance action available for " + filterComponents[0])
		} else if len(filterComponents) > 1 {
			d.statusMsg.Set("No automatic maintenance action available for selected components")
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
	d.prepareAutomaticFixes([]string{comp})
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
	d.prepareAutomaticFixes(nil)
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
