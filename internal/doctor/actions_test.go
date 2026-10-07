package doctor

import (
	"path/filepath"
	"strings"
	"testing"
)

func validTestProposal() ActionProposal {
	return ActionProposal{
		ID: "update", Key: FindingKey{IntegrationID: "fixture", CheckID: "version", InstanceID: "instance"},
		Mode: ActionAutomatic, Label: "Update fixture", Reason: "A supported update is available",
		TargetIDs: []string{"instance"}, VerificationCheckID: "version",
		Steps: []CommandStep{{Label: "Update selected target", Command: Command{Executable: filepath.Join(string(filepath.Separator), "fixture"), Args: []string{"update", "target"}}}},
	}
}

func TestProposalLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		change    func(*ActionProposal)
		wantError bool
	}{
		{"valid", func(*ActionProposal) {}, false},
		{"too many steps", func(p *ActionProposal) { p.Steps = make([]CommandStep, 33) }, true},
		{"too many targets", func(p *ActionProposal) { p.TargetIDs = make([]string, 129) }, true},
		{"too many arguments", func(p *ActionProposal) { p.Steps[0].Command.Args = make([]string, 129) }, true},
		{"argument bytes", func(p *ActionProposal) { p.Steps[0].Command.Args = []string{strings.Repeat("x", 32769)} }, true},
		{"argument byte boundary", func(p *ActionProposal) { p.Steps[0].Command.Args = []string{strings.Repeat("x", 32768)} }, false},
		{"nul argument", func(p *ActionProposal) { p.Steps[0].Command.Args = []string{"target\x00injection"} }, true},
		{"relative executable", func(p *ActionProposal) { p.Steps[0].Command.Executable = "fixture" }, true},
		{"relative directory", func(p *ActionProposal) { p.Steps[0].Command.Dir = "relative" }, true},
		{"shell interpreter", func(p *ActionProposal) { p.Steps[0].Command.Executable = "/bin/sh" }, true},
		{"implicit privilege", func(p *ActionProposal) { p.Steps[0].Command.Executable = "/usr/bin/sudo" }, true},
		{"duplicate target", func(p *ActionProposal) { p.TargetIDs = []string{"instance", "instance"} }, true},
		{"missing target", func(p *ActionProposal) { p.TargetIDs = nil }, true},
		{"missing verification", func(p *ActionProposal) { p.VerificationCheckID = "" }, true},
		{"missing step", func(p *ActionProposal) { p.Steps = nil }, true},
		{"oversized label", func(p *ActionProposal) { p.Label = strings.Repeat("x", 2049) }, true},
		{"terminal control", func(p *ActionProposal) { p.Label = "safe\x1b[2J" }, true},
		{"unknown mode", func(p *ActionProposal) { p.Mode = "" }, true},
		{"manual no commands", func(p *ActionProposal) {
			p.Mode = ActionManual
			p.Steps = nil
			p.TargetIDs = nil
			p.VerificationCheckID = ""
		}, false},
		{"manual executable", func(p *ActionProposal) { p.Mode = ActionManual }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validTestProposal()
			tt.change(&p)
			err := ValidateProposal(p, DefaultLimits())
			if (err != nil) != tt.wantError {
				t.Fatalf("proposal error = %v, want error %t", err, tt.wantError)
			}
		})
	}
}
