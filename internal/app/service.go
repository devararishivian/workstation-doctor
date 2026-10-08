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
