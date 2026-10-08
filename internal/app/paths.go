// Package app contains application-level use cases and workstation state paths.
package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const historyDirectoryName = "workstation-doctor"

// HistoryPaths identifies the current action history, stable ownership state,
// and the previous database location without creating any of them.
type HistoryPaths struct {
	Database       string
	StateDir       string
	LegacyDatabase string
}

// ResolveHistoryPaths applies platform defaults and an optional database-only override.
// The ownership state directory remains stable when the database is overridden.
func ResolveHistoryPaths(osName, home string, env map[string]string, override string) (HistoryPaths, error) {
	if !filepath.IsAbs(home) || strings.TrimSpace(home) == "" {
		return HistoryPaths{}, errors.New("home directory must be an absolute path")
	}
	home = filepath.Clean(home)

	var stateRoot string
	switch osName {
	case "darwin":
		stateRoot = filepath.Join(home, "Library", "Application Support")
	case "linux":
		stateRoot = filepath.Join(home, ".local", "state")
		if candidate := strings.TrimSpace(env["XDG_STATE_HOME"]); filepath.IsAbs(candidate) {
			stateRoot = filepath.Clean(candidate)
		}
	default:
		return HistoryPaths{}, fmt.Errorf("history paths are unsupported on %q", osName)
	}

	stateDir := filepath.Join(stateRoot, historyDirectoryName)
	database := filepath.Join(stateDir, "doctor.db")
	if strings.TrimSpace(override) != "" {
		var err error
		database, err = filepath.Abs(override)
		if err != nil {
			return HistoryPaths{}, fmt.Errorf("resolve history database override: %w", err)
		}
		database = filepath.Clean(database)
	}

	return HistoryPaths{
		Database:       database,
		StateDir:       stateDir,
		LegacyDatabase: filepath.Join(home, ".local", "share", historyDirectoryName, "doctor.db"),
	}, nil
}
