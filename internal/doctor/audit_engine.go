package doctor

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"
)

// AuditEngine schedules explicitly registered read-only algorithms.
type AuditEngine struct {
	definitions []Definition
	limits      Limits
}

// NewAuditEngine validates and freezes registration without discovering products.
func NewAuditEngine(defs []Definition, limits Limits) (*AuditEngine, error) {
	if err := ValidateDefinitions(defs); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	frozen := slices.Clone(defs)
	for i := range frozen {
		frozen[i].Checks = slices.Clone(defs[i].Checks)
		frozen[i].Integration.References = slices.Clone(defs[i].Integration.References)
		frozen[i].Integration.SupportedScopes = slices.Clone(defs[i].Integration.SupportedScopes)
	}
	return &AuditEngine{definitions: frozen, limits: limits}, nil
}

func cloneScope(scope Scope) Scope {
	clone := Scope{ProjectDir: scope.ProjectDir, Locations: map[string][]string{}, SkillRoots: slices.Clone(scope.SkillRoots)}
	for id, paths := range scope.Locations {
		clone.Locations[id] = slices.Clone(paths)
	}
	return clone
}

func freshHost(host *Host) *Host {
	if host == nil {
		return &Host{Now: time.Now, inventory: &auditInventory{}}
	}
	now := host.Now
	if now == nil {
		now = time.Now
	}
	return &Host{
		OS: host.OS, Home: host.Home, Path: host.Path, Env: maps.Clone(host.Env),
		Now: now, RunRead: host.RunRead, Fetch: host.Fetch, inventory: &auditInventory{},
	}
}

func cloneInstance(instance Instance) Instance {
	instance.Capabilities = slices.Clone(instance.Capabilities)
	instance.Configuration = slices.Clone(instance.Configuration)
	instance.Diagnostics = slices.Clone(instance.Diagnostics)
	if instance.InstalledAt != nil {
		evidence := *instance.InstalledAt
		instance.InstalledAt = &evidence
	}
	return instance
}

func (e *AuditEngine) descriptors() ([]Integration, []CheckDescriptor) {
	integrations := make([]Integration, len(e.definitions))
	var checks []CheckDescriptor
	for i, def := range e.definitions {
		integrations[i] = def.Integration
		integrations[i].References = slices.Clone(def.Integration.References)
		integrations[i].SupportedScopes = slices.Clone(def.Integration.SupportedScopes)
		for _, check := range def.Checks {
			checks = append(checks, CheckDescriptor{
				IntegrationID: def.Integration.ID, ID: check.ID, Name: check.Name, Question: check.Question, Order: check.Order,
			})
		}
	}
	slices.SortFunc(checks, func(a, b CheckDescriptor) int { return a.Order - b.Order })
	return integrations, checks
}

// Audit shares discovery within one audit and preserves registered check order.
func (e *AuditEngine) Audit(ctx context.Context, host *Host, scope Scope) AuditReport {
	ctx, cancel := context.WithTimeout(ctx, e.limits.AuditTimeout)
	defer cancel()
	observed := freshHost(host)
	if observed.Now == nil {
		observed.Now = time.Now
	}
	integrations, checks := e.descriptors()
	observed.inventory.discoveries = make([]Discovery, len(e.definitions))
	report := AuditReport{
		StartedAt: observed.Now(), Scope: cloneScope(scope), Integrations: integrations, Checks: checks,
		Discoveries: observed.inventory.discoveries,
	}
	for i := range report.Discoveries {
		report.Discoveries[i].Availability = AvailabilityUndetermined
	}
	e.parallel(ctx, len(e.definitions), func(i int) {
		report.Discoveries[i] = e.definitions[i].Discover(ctx, observed, cloneScope(scope))
		e.normalizeDiscovery(&report.Discoveries[i], e.definitions[i].Integration.ID)
	})
	type checkJob struct {
		definition, instance int
		check                CheckDefinition
	}
	var jobs []checkJob
	for i, def := range e.definitions {
		for _, check := range def.Checks {
			for j := 0; j < max(1, len(report.Discoveries[i].Instances)); j++ {
				jobs = append(jobs, checkJob{i, j, check})
			}
			if report.Discoveries[i].Availability == AvailabilityUndetermined && len(report.Discoveries[i].Instances) > 0 {
				jobs = append(jobs, checkJob{i, -1, check})
			}
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].check.Order < jobs[j].check.Order })
	results := make([][]Finding, len(jobs))
	for i, job := range jobs {
		instanceID := ""
		if job.instance >= 0 && job.instance < len(report.Discoveries[job.definition].Instances) {
			instanceID = report.Discoveries[job.definition].Instances[job.instance].ID
		}
		results[i] = []Finding{unavailableFinding(e.definitions[job.definition].Integration.ID, job.check, instanceID, OutcomeCanceled, "Inspection was canceled before completion.")}
	}
	e.parallel(ctx, len(jobs), func(i int) {
		job := jobs[i]
		discovery := report.Discoveries[job.definition]
		id := e.definitions[job.definition].Integration.ID
		if job.instance < 0 {
			results[i] = []Finding{unavailableFinding(id, job.check, "", OutcomeUnknown, "Discovery coverage is incomplete; completed instances remain inspectable.")}
			return
		}
		if len(discovery.Instances) == 0 {
			outcome := availabilityOutcome(discovery.Availability)
			results[i] = []Finding{unavailableFinding(id, job.check, "", outcome, "No applicable instance was established within the selected scope.")}
			return
		}
		instance := discovery.Instances[job.instance]
		if instance.Availability != AvailabilityPresent {
			results[i] = []Finding{unavailableFinding(id, job.check, instance.ID, availabilityOutcome(instance.Availability), "Instance availability could not support this inspection.")}
			return
		}
		results[i] = evaluate(ctx, observed, cloneScope(scope), id, job.check, instance)
	})
	for _, findings := range results {
		report.Findings = append(report.Findings, findings...)
	}
	report.Canceled = ctx.Err() != nil
	report.FinishedAt = observed.Now()
	return report
}

func (e *AuditEngine) parallel(ctx context.Context, count int, run func(int)) {
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(count, e.limits.MaxConcurrency) {
		workers.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				run(index)
			}
		})
	}
	defer workers.Wait()
	defer close(jobs)
	for i := range count {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case jobs <- i:
		}
	}
}

func (e *AuditEngine) normalizeDiscovery(discovery *Discovery, integrationID string) {
	discovery.Diagnostics = slices.Clone(discovery.Diagnostics)
	if len(discovery.Instances) > e.limits.MaxFiles {
		discovery.Instances = discovery.Instances[:e.limits.MaxFiles]
		discovery.Availability = AvailabilityUndetermined
		discovery.Diagnostics = append(discovery.Diagnostics, "Discovered instance limit reached; coverage is incomplete.")
	}
	switch discovery.Availability {
	case AvailabilityPresent, AvailabilityAbsent, AvailabilityUnsupported, AvailabilityUndetermined:
	default:
		discovery.Availability = AvailabilityUndetermined
	}
	discovery.Instances = slices.Clone(discovery.Instances)
	counts := map[string]int{}
	for _, instance := range discovery.Instances {
		counts[instance.ID]++
	}
	for i := range discovery.Instances {
		discovery.Instances[i] = cloneInstance(discovery.Instances[i])
		instance := &discovery.Instances[i]
		if instance.IntegrationID == "" {
			instance.IntegrationID = integrationID
		}
		if instance.Availability == "" {
			instance.Availability = AvailabilityUndetermined
		}
		if !validIdentifier(instance.ID, e.limits.MaxIdentifierBytes) || counts[instance.ID] > 1 || instance.IntegrationID != integrationID {
			instance.Availability = AvailabilityUndetermined
			instance.Diagnostics = append(instance.Diagnostics, "Discovery returned an invalid or duplicate instance identity.")
		}
	}
}

func availabilityOutcome(availability Availability) Outcome {
	switch availability {
	case AvailabilityAbsent, AvailabilityUnsupported:
		return OutcomeNotApplicable
	default:
		return OutcomeUnknown
	}
}

func unavailableFinding(integrationID string, check CheckDefinition, instanceID string, outcome Outcome, explanation string) Finding {
	return Finding{
		Key:     FindingKey{IntegrationID: integrationID, CheckID: check.ID, InstanceID: instanceID},
		Outcome: outcome, Question: check.Question, Explanation: explanation,
	}
}

func evaluate(ctx context.Context, host *Host, scope Scope, integrationID string, check CheckDefinition, instance Instance) []Finding {
	findings := slices.Clone(check.Evaluate(ctx, host, scope, cloneInstance(instance)))
	if len(findings) == 0 {
		outcome := OutcomeUnknown
		if ctx.Err() != nil {
			outcome = OutcomeCanceled
		}
		return []Finding{unavailableFinding(integrationID, check, instance.ID, outcome, "The check did not establish an answer.")}
	}
	for i := range findings {
		findings[i].Key = FindingKey{IntegrationID: integrationID, CheckID: check.ID, InstanceID: instance.ID}
		findings[i].Question = check.Question
		findings[i].Evidence = slices.Clone(findings[i].Evidence)
		findings[i].References = slices.Clone(findings[i].References)
		findings[i].Actions = slices.Clone(findings[i].Actions)
		switch findings[i].Outcome {
		case OutcomeOK, OutcomeAttention, OutcomeUnknown, OutcomeNotApplicable, OutcomeCanceled:
		default:
			findings[i].Outcome = OutcomeUnknown
		}
		for j := range findings[i].Actions {
			action := &findings[i].Actions[j]
			action.Key = findings[i].Key
			action.TargetIDs = slices.Clone(action.TargetIDs)
			action.Preconditions = slices.Clone(action.Preconditions)
			action.SideEffects = slices.Clone(action.SideEffects)
			action.Steps = slices.Clone(action.Steps)
			for k := range action.Steps {
				action.Steps[k].Command.Args = slices.Clone(action.Steps[k].Command.Args)
				action.Steps[k].Command.Env = maps.Clone(action.Steps[k].Command.Env)
			}
		}
	}
	return findings
}

// Inspect freshly discovers and evaluates a registered target, never replaying data.
func (e *AuditEngine) Inspect(ctx context.Context, host *Host, scope Scope, key FindingKey) (Finding, error) {
	if err := ctx.Err(); err != nil {
		return Finding{}, fmt.Errorf("inspect target: %w", err)
	}
	observed := freshHost(host)
	observed.inventory.discoveries = make([]Discovery, 1)
	ctx, cancel := context.WithTimeout(ctx, e.limits.AuditTimeout)
	defer cancel()
	for _, def := range e.definitions {
		if def.Integration.ID != key.IntegrationID {
			continue
		}
		for _, check := range def.Checks {
			if check.ID != key.CheckID {
				continue
			}
			discovery := def.Discover(ctx, observed, cloneScope(scope))
			if err := ctx.Err(); err != nil {
				return Finding{}, fmt.Errorf("discover inspection target: %w", err)
			}
			e.normalizeDiscovery(&discovery, def.Integration.ID)
			observed.inventory.discoveries[0] = discovery
			for _, instance := range discovery.Instances {
				if instance.ID != key.InstanceID {
					continue
				}
				if instance.Availability != AvailabilityPresent {
					return unavailableFinding(def.Integration.ID, check, instance.ID, availabilityOutcome(instance.Availability), "Instance is no longer available for inspection."), nil
				}
				findings := evaluate(ctx, observed, cloneScope(scope), def.Integration.ID, check, instance)
				if err := ctx.Err(); err != nil {
					return Finding{}, fmt.Errorf("evaluate inspection target: %w", err)
				}
				if len(findings) != 1 {
					return Finding{}, errors.New("inspection target produced ambiguous findings; discover individual resource instances")
				}
				return findings[0], nil
			}
			if key.InstanceID == "" {
				return unavailableFinding(def.Integration.ID, check, "", availabilityOutcome(discovery.Availability), "No applicable instance was established."), nil
			}
			return Finding{}, errors.New("inspection instance is no longer present")
		}
	}
	return Finding{}, fmt.Errorf("inspection check is not registered")
}
