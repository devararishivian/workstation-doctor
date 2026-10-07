package main

import (
	"context"
	"testing"
	"workstation-doctor/internal/doctor"

	tui "github.com/grindlemire/go-tui"
	"github.com/urfave/cli/v3"
)

func TestMenuMaintenanceBlocked(t *testing.T) {
	for _, name := range []string{"selection", "confirmation"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Legacy RED execution cannot start subprocesses.
			d := &doctorApp{
				ctx: ctx, cmd: &cli.Command{},
				mode: tui.NewState("menu"), statusMsg: tui.NewState(""),
				results: tui.NewState([]doctor.Result{}), runID: tui.NewState(int64(0)),
			}
			if name == "selection" {
				d.runAction("fix")
			} else {
				d.executeFix()
			}
			if got := d.mode.Get(); got != "menu" {
				t.Errorf("mode = %q, want menu", got)
			}
			if got := d.statusMsg.Get(); got != "Automatic maintenance is unavailable during the architecture migration." {
				t.Errorf("blocked maintenance status = %q", got)
			}
		})
	}
}
