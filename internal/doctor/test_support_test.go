package doctor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testHost(t *testing.T) *Host {
	t.Helper()
	home := t.TempDir()
	return &Host{
		OS: "darwin", Home: home, Path: home, Env: map[string]string{"HOME": home},
		Now: func() time.Time { return time.Unix(100, 0).UTC() },
		RunRead: func(context.Context, Command) (CommandResult, error) {
			return CommandResult{ExitCode: -1}, errors.New("unexpected inspection command")
		},
		Fetch: func(context.Context, string) ([]byte, error) { return nil, errors.New("unexpected metadata request") },
	}
}

func testAuditEngine(t *testing.T, defs []Definition, limits Limits) *AuditEngine {
	t.Helper()
	engine, err := NewAuditEngine(defs, limits)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func fixtureDiscovery(context.Context, *Host, Scope) Discovery {
	return Discovery{Availability: AvailabilityPresent, Instances: []Instance{{ID: "instance", IntegrationID: "fixture", Availability: AvailabilityPresent}}}
}
