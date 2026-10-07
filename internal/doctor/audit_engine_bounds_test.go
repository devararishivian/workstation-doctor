package doctor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestAuditFreshInventory(t *testing.T) {
	t.Parallel()
	host := testHost(t)
	host.inventory = &auditInventory{discoveries: []Discovery{{Availability: AvailabilityPresent}}}
	seen := map[*auditInventory]bool{}
	def := validTestDefinition()
	def.Discover = func(ctx context.Context, observed *Host, scope Scope) Discovery {
		if observed.inventory == nil || observed.inventory == host.inventory || seen[observed.inventory] {
			t.Error("inventory ownership was reused across inspection calls")
		}
		seen[observed.inventory] = true
		return fixtureDiscovery(ctx, observed, scope)
	}
	def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding { return []Finding{{Outcome: OutcomeOK}} }
	engine := testAuditEngine(t, []Definition{def}, DefaultLimits())
	first := engine.Audit(t.Context(), host, Scope{})
	engine.Audit(t.Context(), host, Scope{})
	if _, err := engine.Inspect(t.Context(), host, Scope{}, first.Findings[0].Key); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || host.inventory.discoveries[0].Availability != AvailabilityPresent {
		t.Fatal("fresh calls changed caller inventory")
	}
}

func TestAuditBoundsDiscoveredWork(t *testing.T) {
	t.Parallel()
	limits := DefaultLimits()
	limits.MaxFiles = 2
	def := validTestDefinition()
	def.Discover = func(context.Context, *Host, Scope) Discovery {
		return Discovery{Availability: AvailabilityPresent, Instances: []Instance{
			{ID: "a", Availability: AvailabilityPresent}, {ID: "b", Availability: AvailabilityPresent}, {ID: "c", Availability: AvailabilityPresent},
		}}
	}
	def.Checks[0].Evaluate = func(context.Context, *Host, Scope, Instance) []Finding { return []Finding{{Outcome: OutcomeOK}} }
	report := testAuditEngine(t, []Definition{def}, limits).Audit(t.Context(), testHost(t), Scope{})
	if len(report.Discoveries[0].Instances) != 2 {
		t.Fatalf("unbounded discovered work: %d", len(report.Discoveries[0].Instances))
	}
	var known, unknown int
	for _, finding := range report.Findings {
		if finding.Outcome == OutcomeOK {
			known++
		}
		if finding.Outcome == OutcomeUnknown {
			unknown++
		}
	}
	if known != 2 || unknown != 1 || len(report.Discoveries[0].Diagnostics) == 0 {
		t.Fatalf("limit exhaustion lost completed evidence or partial coverage: %+v", report)
	}
}

func TestAuditIsolatesEvidence(t *testing.T) {
	t.Parallel()
	def := validTestDefinition()
	instance := Instance{ID: "instance", Availability: AvailabilityPresent, Configuration: []Fact{{State: EvidenceKnown, Value: "observed"}}}
	def.Discover = func(context.Context, *Host, Scope) Discovery {
		return Discovery{Availability: AvailabilityPresent, Instances: []Instance{instance}}
	}
	def.Checks[0].Evaluate = func(_ context.Context, _ *Host, scope Scope, instance Instance) []Finding {
		instance.Configuration[0].Value = "modified"
		scope.Locations["fixture"][0] = "modified"
		return []Finding{{Outcome: OutcomeOK}}
	}
	scope := Scope{Locations: map[string][]string{"fixture": {"declared"}}}
	report := testAuditEngine(t, []Definition{def}, DefaultLimits()).Audit(t.Context(), testHost(t), scope)
	if report.Discoveries[0].Instances[0].Configuration[0].Value != "observed" || instance.Configuration[0].Value != "observed" || scope.Locations["fixture"][0] != "declared" {
		t.Fatal("check mutated discovery evidence or caller scope")
	}
}

func TestAuditDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		limits := DefaultLimits()
		limits.AuditTimeout = time.Second
		var reached atomic.Bool
		def := validTestDefinition()
		def.Discover = fixtureDiscovery
		def.Checks[0].Evaluate = func(ctx context.Context, _ *Host, _ Scope, _ Instance) []Finding {
			<-ctx.Done()
			reached.Store(errors.Is(ctx.Err(), context.DeadlineExceeded))
			return nil
		}
		host := &Host{Now: time.Now}
		report := testAuditEngine(t, []Definition{def}, limits).Audit(t.Context(), host, Scope{})
		if !reached.Load() || !report.Canceled || report.Findings[0].Outcome != OutcomeCanceled || report.FinishedAt.Sub(report.StartedAt) != time.Second {
			t.Fatalf("audit deadline did not propagate: %+v", report)
		}
	})
}
