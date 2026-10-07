package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestAuditDiscoveryOnce(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	def := validTestDefinition()
	def.Discover = func(ctx context.Context, host *Host, scope Scope) Discovery {
		calls.Add(1)
		return fixtureDiscovery(ctx, host, scope)
	}
	def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding { return []Finding{{Outcome: OutcomeOK}} }
	other := def.Checks[0]
	other.ID = "configuration"
	other.Order = 2
	def.Checks = append(def.Checks, other)
	engine := testAuditEngine(t, []Definition{def}, DefaultLimits())
	host := testHost(t)
	report := engine.Audit(t.Context(), host, Scope{})
	if calls.Load() != 1 || len(report.Findings) != 2 || len(report.Checks) != 2 {
		t.Fatalf("unexpected report: %+v; discovery calls %d", report, calls.Load())
	}
	if _, err := engine.Inspect(t.Context(), host, Scope{}, report.Findings[0].Key); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("Inspect reused stale discovery")
	}
}

func TestAuditDeterministicOrder(t *testing.T) {
	t.Parallel()
	def := validTestDefinition()
	def.Discover = fixtureDiscovery
	completed := make(chan struct{})
	def.Checks = []CheckDefinition{
		{ID: "first", Name: "First", Question: "First?", Order: 1, Evaluate: func(ctx context.Context, _ *Host, _ Scope, _ Instance) []Finding {
			select {
			case <-completed:
			case <-ctx.Done():
			}
			return []Finding{{Outcome: OutcomeOK}}
		}},
		{ID: "second", Name: "Second", Question: "Second?", Order: 2, Evaluate: func(context.Context, *Host, Scope, Instance) []Finding {
			close(completed)
			return []Finding{{Outcome: OutcomeOK}}
		}},
	}
	report := testAuditEngine(t, []Definition{def}, DefaultLimits()).Audit(t.Context(), testHost(t), Scope{})
	got := []string{report.Findings[0].Key.CheckID, report.Findings[1].Key.CheckID}
	if !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("finding order: %v", got)
	}
}

func TestAuditCanceledBeforeStart(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	def := validTestDefinition()
	def.Discover = func(context.Context, *Host, Scope) Discovery { calls.Add(1); return Discovery{} }
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report := testAuditEngine(t, []Definition{def}, DefaultLimits()).Audit(ctx, testHost(t), Scope{})
	if !report.Canceled || calls.Load() != 0 || len(report.Findings) != 1 || report.Findings[0].Outcome != OutcomeCanceled {
		t.Fatalf("pre-canceled report = %+v; calls %d", report, calls.Load())
	}
}

func TestAuditAvailability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		availability Availability
		want         Outcome
	}{
		{"absent", AvailabilityAbsent, OutcomeNotApplicable},
		{"unsupported", AvailabilityUnsupported, OutcomeNotApplicable},
		{"undetermined", AvailabilityUndetermined, OutcomeUnknown},
		{"unspecified", "", OutcomeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validTestDefinition()
			def.Discover = func(context.Context, *Host, Scope) Discovery { return Discovery{Availability: tt.availability} }
			report := testAuditEngine(t, []Definition{def}, DefaultLimits()).Audit(t.Context(), testHost(t), Scope{})
			if len(report.Findings) != 1 || report.Findings[0].Outcome != tt.want {
				t.Fatalf("report = %+v, want %s", report, tt.want)
			}
		})
	}
}

func TestAuditNoStorage(t *testing.T) {
	t.Parallel()
	host := testHost(t)
	report := testAuditEngine(t, []Definition{validTestDefinition()}, DefaultLimits()).Audit(t.Context(), host, Scope{})
	if len(report.Integrations) != 1 {
		t.Fatal("absent integration lost its descriptor")
	}
	entries, err := os.ReadDir(host.Home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("audit changed temporary home: %v, %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(host.Home, "doctor.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("audit created storage: %v", err)
	}
}

func TestAuditConcurrencyLimit(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxConcurrency = 2
		var active, peak atomic.Int32
		release := make(chan struct{})
		def := validTestDefinition()
		def.Discover = func(context.Context, *Host, Scope) Discovery {
			return Discovery{Availability: AvailabilityPresent, Instances: []Instance{
				{ID: "a", Availability: AvailabilityPresent},
				{ID: "b", Availability: AvailabilityPresent},
				{ID: "c", Availability: AvailabilityPresent},
				{ID: "d", Availability: AvailabilityPresent},
			}}
		}
		def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding {
			n := active.Add(1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			<-release
			active.Add(-1)
			return []Finding{{Outcome: OutcomeOK}}
		}
		host := &Host{Now: func() time.Time { return time.Unix(100, 0) }}
		engine := testAuditEngine(t, []Definition{def}, limits)
		done := make(chan AuditReport, 1)
		go func() { done <- engine.Audit(t.Context(), host, Scope{}) }()
		synctest.Wait()
		if count := active.Load(); count != 2 {
			close(release)
			<-done
			t.Fatalf("active checks = %d, want 2", count)
		}
		close(release)
		report := <-done
		if peak.Load() != 2 || len(report.Findings) != 4 {
			t.Fatalf("peak = %d, report = %+v", peak.Load(), report)
		}
	})
}

func TestAuditDuplicateInstancesUnknown(t *testing.T) {
	t.Parallel()
	def := validTestDefinition()
	def.Discover = func(context.Context, *Host, Scope) Discovery {
		return Discovery{Availability: AvailabilityPresent, Instances: []Instance{
			{ID: "duplicate", Availability: AvailabilityPresent}, {ID: "duplicate", Availability: AvailabilityPresent},
		}}
	}
	def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding { return []Finding{{Outcome: OutcomeOK}} }
	report := testAuditEngine(t, []Definition{def}, DefaultLimits()).Audit(t.Context(), testHost(t), Scope{})
	for _, finding := range report.Findings {
		if finding.Outcome != OutcomeUnknown {
			t.Fatalf("duplicate instance falsely inspected as healthy: %+v", finding)
		}
	}
}

func TestAuditRetainsCompletedAfterCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	limits := DefaultLimits()
	limits.MaxConcurrency = 1
	def := validTestDefinition()
	def.Discover = fixtureDiscovery
	def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding {
		cancel()
		return []Finding{{Outcome: OutcomeOK}}
	}
	second := def.Checks[0]
	second.ID = "second"
	second.Order = 2
	second.Evaluate = func(context.Context, *Host, Scope, Instance) []Finding {
		t.Error("scheduled after cancellation")
		return nil
	}
	def.Checks = append(def.Checks, second)
	report := testAuditEngine(t, []Definition{def}, limits).Audit(ctx, testHost(t), Scope{})
	if !report.Canceled || len(report.Findings) != 2 || report.Findings[0].Outcome != OutcomeOK || report.Findings[1].Outcome != OutcomeCanceled {
		t.Fatalf("completed work was lost: %+v", report)
	}
	if report.Findings[1].Key.InstanceID != "instance" {
		t.Fatalf("canceled work lost target identity: %+v", report.Findings[1])
	}
}

func TestInspectionCanceledEvaluation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	def := validTestDefinition()
	def.Discover = fixtureDiscovery
	def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding {
		cancel()
		return []Finding{{Outcome: OutcomeOK}}
	}
	engine := testAuditEngine(t, []Definition{def}, DefaultLimits())
	_, err := engine.Inspect(ctx, testHost(t), Scope{}, FindingKey{IntegrationID: "fixture", CheckID: "version", InstanceID: "instance"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection verification ignored cancellation: %v", err)
	}
}
