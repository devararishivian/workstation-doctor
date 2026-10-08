package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"workstation-doctor/internal/app"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

func syntheticServiceForMenu(t *testing.T) (*app.Service, *atomic.Int32) {
	t.Helper()
	var openCalls atomic.Int32
	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(context.Context, *doctor.Host, doctor.Scope, doctor.Instance) []doctor.Finding {
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK, Explanation: "Fixture check OK"}}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
		OpenHistory: func(context.Context, string, store.Policy) (app.History, error) {
			openCalls.Add(1)
			return nil, errors.New("open history unexpected")
		},
	})
	return service, &openCalls
}

func TestMenuAuditUsesService(t *testing.T) {
	service, openCalls := syntheticServiceForMenu(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d := newDoctorApp(ctx, service)
	if d.service != service {
		t.Fatal("doctorApp does not retain service")
	}

	d.startAudit()

	// Wait for audit to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.mode.Get() == "results" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if d.mode.Get() != "results" {
		t.Fatalf("expected mode results after audit, got %q", d.mode.Get())
	}
	report := d.report.Get()
	if len(report.Findings) != 1 || report.Findings[0].Outcome != doctor.OutcomeOK {
		t.Fatalf("expected 1 OK finding from service, got %+v", report)
	}
	if openCalls.Load() != 0 {
		t.Fatalf("audit opened history %d times", openCalls.Load())
	}
}

func TestMenuGenerationGuardsLateResults(t *testing.T) {
	// First audit blocks until unblocked; second audit runs immediately
	blockFirst := make(chan struct{})
	firstStarted := make(chan struct{})
	var auditCount atomic.Int32

	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(_ context.Context, _ *doctor.Host, _ doctor.Scope, _ doctor.Instance) []doctor.Finding {
				count := auditCount.Add(1)
				if count == 1 {
					close(firstStarted)
					<-blockFirst
					return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeAttention, Explanation: "Slow first audit"}}
				}
				return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK, Explanation: "Fast second audit"}}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := newDoctorApp(ctx, service)

	// Start first audit (slow)
	d.startAudit()
	<-firstStarted

	// Start second audit (fast)
	d.startAudit()

	// Wait for second audit to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.mode.Get() == "results" && len(d.report.Get().Findings) > 0 && d.report.Get().Findings[0].Outcome == doctor.OutcomeOK {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.report.Get().Findings[0].Outcome != doctor.OutcomeOK {
		t.Fatalf("expected second audit outcome OK, got %v", d.report.Get().Findings[0].Outcome)
	}

	// Now unblock the first audit. Since its generation is older, it must not overwrite the second audit results!
	close(blockFirst)
	time.Sleep(50 * time.Millisecond)

	if d.report.Get().Findings[0].Outcome != doctor.OutcomeOK {
		t.Fatalf("late first audit overwrote newer generation results: %+v", d.report.Get().Findings[0])
	}
}

func TestMenuQuitCancelsWork(t *testing.T) {
	blockAudit := make(chan struct{})
	auditStarted := make(chan struct{})

	key := doctor.FindingKey{IntegrationID: "fixture", CheckID: "check", InstanceID: "inst"}
	engine, err := doctor.NewAuditEngine([]doctor.Definition{{
		Integration: doctor.Integration{ID: "fixture", Name: "Fixture", Description: "Desc"},
		Discover: func(context.Context, *doctor.Host, doctor.Scope) doctor.Discovery {
			return doctor.Discovery{Availability: doctor.AvailabilityPresent, Instances: []doctor.Instance{{
				ID: "inst", IntegrationID: "fixture", Availability: doctor.AvailabilityPresent,
			}}}
		},
		Checks: []doctor.CheckDefinition{{
			ID: "check", Name: "Check", Question: "OK?", Order: 1,
			Evaluate: func(ctx context.Context, _ *doctor.Host, _ doctor.Scope, _ doctor.Instance) []doctor.Finding {
				close(auditStarted)
				select {
				case <-ctx.Done():
					return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeCanceled}}
				case <-blockAudit:
					return []doctor.Finding{{Key: key, Outcome: doctor.OutcomeOK}}
				}
			},
		}},
	}}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	host, err := doctor.NewHost(doctor.Scope{}, doctor.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(engine, host, doctor.Scope{}, app.Options{
		StateDir: t.TempDir(),
		DBPath:   t.TempDir() + "/db.sqlite",
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := newDoctorApp(ctx, service)

	d.startAudit()
	<-auditStarted

	// Calling stop() must cancel running audit work promptly
	d.stop()

	select {
	case <-d.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("doctorApp context was not canceled on stop()")
	}
}
