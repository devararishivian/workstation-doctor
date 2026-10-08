package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

// HistoryWriter provides store write access for action lifecycle tracking.
type HistoryWriter interface {
	StartAction(ctx context.Context, start store.ActionStart) error
	SaveStep(ctx context.Context, actionID string, step store.StepResult) error
	FinishAction(ctx context.Context, actionID string, finish store.ActionFinish) error
}

// HistoryReader provides query access for maintenance history.
type HistoryReader interface {
	ListActions(ctx context.Context, query store.ActionQuery) (store.ActionPage, error)
	Action(ctx context.Context, id string) (store.ActionRecord, error)
}

// HistoryMaintenance provides maintenance and lifecycle control over history storage.
type HistoryMaintenance interface {
	Prune(ctx context.Context, now time.Time) (int64, error)
	Close() error
}

// History embeds the consumer-owned history capabilities.
type History interface {
	HistoryWriter
	HistoryReader
	HistoryMaintenance
}

// Options provides configuration and test seams for the application service.
type Options struct {
	Version     string
	DBPath      string
	StateDir    string
	Policy      store.Policy
	OpenHistory func(context.Context, string, store.Policy) (History, error)
	RunStep     func(context.Context, doctor.CommandStep) (store.StepResult, error)
	Acquire     func(context.Context, string) (Ownership, error)
}

// Service coordinates safe workstation inspections and maintenance actions.
type Service struct {
	engine  *doctor.AuditEngine
	host    *doctor.Host
	scope   doctor.Scope
	options Options

	mu             sync.Mutex
	currentReport  doctor.AuditReport
	usedApprovals  map[string]bool
	validApprovals map[string]string // token -> fingerprint
}

// ActionReport contains the durable execution outcome and verification result.
type ActionReport struct {
	RecordID     string
	Execution    store.ExecutionOutcome
	Verification store.VerificationOutcome
	SafeError    string
	HistoryError error
}

// Apply executes a confirmed action through its complete lifecycle:
// ownership acquisition, durable start recording, precondition revalidation,
// ordered step execution, postcondition verification, and final recording.
func (s *Service) Apply(ctx context.Context, prepared PreparedAction, approval Approval) (ActionReport, error) {
	if err := s.consumeApproval(prepared, approval); err != nil {
		return ActionReport{}, fmt.Errorf("validate approval: %w", err)
	}

	ownership, err := s.options.Acquire(ctx, s.options.StateDir)
	if err != nil {
		return ActionReport{}, fmt.Errorf("acquire maintenance ownership: %w", err)
	}
	defer func() { _ = ownership.Release() }()

	history, err := s.options.OpenHistory(ctx, s.options.DBPath, s.options.Policy)
	if err != nil {
		return ActionReport{}, fmt.Errorf("open history storage: %w", err)
	}
	defer func() { _ = history.Close() }()

	plan := make([]store.PlannedStep, len(prepared.proposal.Steps))
	for i, step := range prepared.proposal.Steps {
		plan[i] = store.PlannedStep{
			Index:       i,
			Label:       step.Label,
			Description: step.Label,
		}
	}

	start := store.ActionStart{
		ID:            prepared.actionID,
		IntegrationID: prepared.key.IntegrationID,
		CheckID:       prepared.key.CheckID,
		InstanceID:    prepared.key.InstanceID,
		Label:         prepared.proposal.Label,
		Kind:          string(prepared.proposal.Mode),
		Reason:        prepared.proposal.Reason,
		AppVersion:    s.options.Version,
		OwnerToken:    ownership.Token(),
		StartedAt:     time.Now().UTC(),
		TargetIDs:     slices.Clone(prepared.proposal.TargetIDs),
		TargetVersion: prepared.proposal.TargetVersion.Value,
		Scope:         prepared.scope.ProjectDir,
		Plan:          plan,
	}

	if err := history.StartAction(ctx, start); err != nil {
		return ActionReport{}, fmt.Errorf("start action record: %w", err)
	}

	// Recheck preconditions
	finding, err := s.engine.Inspect(ctx, s.host, s.scope, prepared.key)
	preconditionsValid := err == nil
	if preconditionsValid {
		var matched *doctor.ActionProposal
		for _, p := range finding.Actions {
			if p.ID == prepared.proposal.ID {
				matched = &p
				break
			}
		}
		if matched == nil || len(matched.Preconditions) != len(prepared.proposal.Preconditions) {
			preconditionsValid = false
		} else {
			for i, pre := range matched.Preconditions {
				if pre.Value != prepared.proposal.Preconditions[i].Value {
					preconditionsValid = false
					break
				}
			}
		}
	}

	if !preconditionsValid {
		finish := store.ActionFinish{
			FinishedAt:   time.Now().UTC(),
			Execution:    store.ExecutionBlocked,
			Verification: store.VerificationNotPerformed,
			SafeError:    "preconditions changed or no longer valid",
		}
		_ = history.FinishAction(ctx, start.ID, finish)
		return ActionReport{
			RecordID:     start.ID,
			Execution:    store.ExecutionBlocked,
			Verification: store.VerificationNotPerformed,
			SafeError:    finish.SafeError,
		}, nil
	}

	// Execute ordered steps
	executionOutcome := store.ExecutionCompleted
	var safeError string
	for i, step := range prepared.proposal.Steps {
		stepResult, runErr := s.options.RunStep(ctx, step)
		stepResult.Index = i

		if err := history.SaveStep(ctx, start.ID, stepResult); err != nil {
			executionOutcome = store.ExecutionFailed
			safeError = fmt.Sprintf("save step checkpoint: %v", err)
			break
		}

		if stepResult.Outcome != store.StepOutcomeCompleted || runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) ||
				stepResult.Outcome == store.StepOutcomeCanceled || ctx.Err() != nil {
				executionOutcome = store.ExecutionCanceled
			} else {
				executionOutcome = store.ExecutionFailed
			}
			safeError = stepResult.SafeError
			if safeError == "" && runErr != nil {
				safeError = runErr.Error()
			}
			break
		}
	}

	// Verification postcondition
	verificationOutcome := store.VerificationNotPerformed
	if executionOutcome == store.ExecutionCompleted && prepared.proposal.VerificationCheckID != "" {
		verifyKey := doctor.FindingKey{
			IntegrationID: prepared.key.IntegrationID,
			CheckID:       prepared.proposal.VerificationCheckID,
			InstanceID:    prepared.key.InstanceID,
		}
		freshFinding, err := s.engine.Inspect(ctx, s.host, s.scope, verifyKey)
		if err != nil {
			verificationOutcome = store.VerificationUnknown
		} else {
			switch freshFinding.Outcome {
			case doctor.OutcomeOK:
				verificationOutcome = store.VerificationPassed
			case doctor.OutcomeAttention:
				verificationOutcome = store.VerificationFailed
			default:
				verificationOutcome = store.VerificationUnknown
			}
		}
	}

	// Finalize action recording using a bounded 5-second context
	finCtx, finCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finCancel()

	finish := store.ActionFinish{
		FinishedAt:   time.Now().UTC(),
		Execution:    executionOutcome,
		Verification: verificationOutcome,
		SafeError:    safeError,
	}
	finishErr := history.FinishAction(finCtx, start.ID, finish)

	return ActionReport{
		RecordID:     start.ID,
		Execution:    executionOutcome,
		Verification: verificationOutcome,
		SafeError:    safeError,
		HistoryError: finishErr,
	}, nil
}

// History queries maintenance history within limits.
func (s *Service) History(ctx context.Context, query store.ActionQuery) (store.ActionPage, error) {
	history, err := s.options.OpenHistory(ctx, s.options.DBPath, s.options.Policy)
	if err != nil {
		return store.ActionPage{}, fmt.Errorf("open history storage: %w", err)
	}
	defer func() { _ = history.Close() }()

	page, err := history.ListActions(ctx, query)
	if err != nil {
		return store.ActionPage{}, fmt.Errorf("list actions: %w", err)
	}
	return page, nil
}

// HistoryDetail fetches a single action record by ID.
func (s *Service) HistoryDetail(ctx context.Context, id string) (store.ActionRecord, error) {
	history, err := s.options.OpenHistory(ctx, s.options.DBPath, s.options.Policy)
	if err != nil {
		return store.ActionRecord{}, fmt.Errorf("open history storage: %w", err)
	}
	defer func() { _ = history.Close() }()

	record, err := history.Action(ctx, id)
	if err != nil {
		return store.ActionRecord{}, fmt.Errorf("get action record: %w", err)
	}
	return record, nil
}

// NewService constructs an application service without opening storage or acquiring locks.
func NewService(engine *doctor.AuditEngine, host *doctor.Host, scope doctor.Scope, options Options) *Service {
	if options.OpenHistory == nil {
		options.OpenHistory = func(ctx context.Context, path string, pol store.Policy) (History, error) {
			s, err := store.OpenHistory(ctx, path, pol)
			if err != nil {
				return nil, fmt.Errorf("open history store: %w", err)
			}
			return s, nil
		}
	}
	if options.RunStep == nil {
		options.RunStep = RunCommandStep
	}
	if options.Acquire == nil {
		options.Acquire = AcquireOwnership
	}
	return &Service{
		engine:         engine,
		host:           host,
		scope:          freezeScope(scope),
		options:        options,
		usedApprovals:  make(map[string]bool),
		validApprovals: make(map[string]string),
	}
}

// Audit runs a fresh whole-workstation inspection and stores an isolated report.
func (s *Service) Audit(ctx context.Context) doctor.AuditReport {
	report := s.engine.Audit(ctx, s.host, s.scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentReport = cloneReport(report)
	return cloneReport(report)
}

// Current returns an isolated snapshot of the most recent audit report.
func (s *Service) Current() doctor.AuditReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneReport(s.currentReport)
}

// Prepare inspects the specific target freshly and freezes an immutable action proposal.
func (s *Service) Prepare(ctx context.Context, key doctor.FindingKey, actionID string) (PreparedAction, error) {
	if actionID == "" {
		return PreparedAction{}, errors.New("action ID is required")
	}
	finding, err := s.engine.Inspect(ctx, s.host, s.scope, key)
	if err != nil {
		return PreparedAction{}, fmt.Errorf("inspect action target: %w", err)
	}

	var matched *doctor.ActionProposal
	for _, proposal := range finding.Actions {
		if proposal.ID == actionID {
			copied := proposal
			matched = &copied
			break
		}
	}
	if matched == nil {
		return PreparedAction{}, fmt.Errorf("action proposal %q not found for target", actionID)
	}

	limits := doctor.DefaultLimits()
	if err := doctor.ValidateProposal(*matched, limits); err != nil {
		return PreparedAction{}, fmt.Errorf("validate proposal: %w", err)
	}

	frozenScope := freezeScope(s.scope)
	frozenProposal := freezeProposal(*matched)
	fingerprint, err := computeActionFingerprint(frozenScope, key, frozenProposal)
	if err != nil {
		return PreparedAction{}, fmt.Errorf("compute action fingerprint: %w", err)
	}

	return PreparedAction{
		actionID:    actionID,
		key:         key,
		proposal:    frozenProposal,
		fingerprint: fingerprint,
		scope:       frozenScope,
	}, nil
}

// Confirm issues a single-use approval token bound to the prepared action's fingerprint.
func (s *Service) Confirm(prepared PreparedAction) (Approval, error) {
	if err := validateFingerprint(prepared); err != nil {
		return Approval{}, fmt.Errorf("confirm action: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if this fingerprint was already approved
	for _, fp := range s.validApprovals {
		if fp == prepared.fingerprint {
			return Approval{}, errors.New("action is already confirmed with an active approval")
		}
	}

	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return Approval{}, fmt.Errorf("generate approval token: %w", err)
	}
	token := hex.EncodeToString(randomBytes)

	s.validApprovals[token] = prepared.fingerprint
	return Approval{
		token:       token,
		fingerprint: prepared.fingerprint,
	}, nil
}

func (s *Service) consumeApproval(prepared PreparedAction, approval Approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if approval.token == "" || approval.fingerprint == "" {
		return errors.New("empty approval token or fingerprint")
	}
	if s.usedApprovals[approval.token] {
		return errors.New("approval token has already been consumed")
	}
	expectedFP, exists := s.validApprovals[approval.token]
	if !exists {
		return errors.New("unrecognized or invalid approval token")
	}
	if expectedFP != prepared.fingerprint || approval.fingerprint != prepared.fingerprint {
		return errors.New("approval token is not bound to this prepared action fingerprint")
	}

	s.usedApprovals[approval.token] = true
	delete(s.validApprovals, approval.token)
	return nil
}

func cloneReport(r doctor.AuditReport) doctor.AuditReport {
	cloned := r
	cloned.Scope = doctor.Scope{
		ProjectDir: r.Scope.ProjectDir,
		Locations:  make(map[string][]string, len(r.Scope.Locations)),
		SkillRoots: slices.Clone(r.Scope.SkillRoots),
	}
	for k, v := range r.Scope.Locations {
		cloned.Scope.Locations[k] = slices.Clone(v)
	}
	cloned.Integrations = slices.Clone(r.Integrations)
	cloned.Checks = slices.Clone(r.Checks)
	cloned.Discoveries = make([]doctor.Discovery, len(r.Discoveries))
	for i, d := range r.Discoveries {
		cloned.Discoveries[i] = doctor.Discovery{
			Availability: d.Availability,
			Instances:    slices.Clone(d.Instances),
			Diagnostics:  slices.Clone(d.Diagnostics),
		}
	}
	cloned.Findings = make([]doctor.Finding, len(r.Findings))
	for i, f := range r.Findings {
		cloned.Findings[i] = doctor.Finding{
			Key:         f.Key,
			Outcome:     f.Outcome,
			Question:    f.Question,
			Explanation: f.Explanation,
			Evidence:    slices.Clone(f.Evidence),
			References:  slices.Clone(f.References),
			Actions:     make([]doctor.ActionProposal, len(f.Actions)),
		}
		for j, a := range f.Actions {
			cloned.Findings[i].Actions[j] = freezeProposal(a)
		}
	}
	return cloned
}

var _ = maps.Clone[map[string]string]
