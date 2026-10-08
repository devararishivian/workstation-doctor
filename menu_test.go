package main

import (
	"context"
	"testing"
)

func TestMenuMaintenanceBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Legacy RED execution cannot start subprocesses.
	d := newDoctorApp(ctx, nil)
	d.runAction("fix")
	if got := d.mode.Get(); got != "menu" {
		t.Errorf("mode = %q, want menu", got)
	}
	if got := d.statusMsg.Get(); got != legacyMaintenanceMessage {
		t.Errorf("blocked maintenance status = %q, want %q", got, legacyMaintenanceMessage)
	}
}
