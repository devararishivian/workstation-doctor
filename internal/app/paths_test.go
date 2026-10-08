package app

import (
	"path/filepath"
	"testing"
)

func TestHistoryPaths(t *testing.T) {
	tests := []struct {
		name       string
		osName     string
		home       string
		env        map[string]string
		override   string
		wantDB     string
		wantState  string
		wantLegacy string
		wantErr    bool
	}{
		{
			name:       "macOS application support default",
			osName:     "darwin",
			home:       "/Users/alice",
			wantDB:     "/Users/alice/Library/Application Support/workstation-doctor/doctor.db",
			wantState:  "/Users/alice/Library/Application Support/workstation-doctor",
			wantLegacy: "/Users/alice/.local/share/workstation-doctor/doctor.db",
		},
		{
			name:       "Linux XDG state path",
			osName:     "linux",
			home:       "/home/alice",
			env:        map[string]string{"XDG_STATE_HOME": "/mnt/state"},
			wantDB:     "/mnt/state/workstation-doctor/doctor.db",
			wantState:  "/mnt/state/workstation-doctor",
			wantLegacy: "/home/alice/.local/share/workstation-doctor/doctor.db",
		},
		{
			name:       "Linux fallback for relative XDG path",
			osName:     "linux",
			home:       "/home/alice",
			env:        map[string]string{"XDG_STATE_HOME": "relative/state"},
			wantDB:     "/home/alice/.local/state/workstation-doctor/doctor.db",
			wantState:  "/home/alice/.local/state/workstation-doctor",
			wantLegacy: "/home/alice/.local/share/workstation-doctor/doctor.db",
		},
		{
			name:       "override changes database but not ownership state",
			osName:     "linux",
			home:       "/home/alice",
			env:        map[string]string{"XDG_STATE_HOME": "/mnt/state"},
			override:   "/tmp/custom-history.db",
			wantDB:     "/tmp/custom-history.db",
			wantState:  "/mnt/state/workstation-doctor",
			wantLegacy: "/home/alice/.local/share/workstation-doctor/doctor.db",
		},
		{
			name:    "unsupported platform",
			osName:  "freebsd",
			home:    "/home/alice",
			wantErr: true,
		},
		{
			name:    "relative home",
			osName:  "linux",
			home:    "home/alice",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveHistoryPaths(tt.osName, tt.home, tt.env, tt.override)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolveHistoryPaths succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveHistoryPaths: %v", err)
			}
			if got.Database != filepath.Clean(tt.wantDB) || got.StateDir != filepath.Clean(tt.wantState) || got.LegacyDatabase != filepath.Clean(tt.wantLegacy) {
				t.Fatalf("ResolveHistoryPaths = %+v, want database=%q state=%q legacy=%q", got, tt.wantDB, tt.wantState, tt.wantLegacy)
			}
		})
	}
}
