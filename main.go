// Command workstation-doctor audits workstation components
// and manages safe workstation maintenance through an interactive TUI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devararishivian/workstation-doctor/internal/app"
	"github.com/devararishivian/workstation-doctor/internal/doctor"
	"github.com/devararishivian/workstation-doctor/internal/store"

	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v3"

	// Embedded SQLite driver for local action history storage.
	_ "modernc.org/sqlite"
)

// Version is set at build time (-ldflags "-X main.version=...").
var version = "1.0.2"

// LaunchOptions holds validated startup configuration for the TUI application.
type LaunchOptions struct {
	DBPath        string
	ProjectDir    string
	Locations     map[string][]string
	SkillRoots    []string
	HistoryPolicy store.Policy
}

func defaultStateDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "state", "workstation-doctor")
	}
	return filepath.Join(os.TempDir(), "workstation-doctor")
}

func isTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

func validateLocations(locations []string) (map[string][]string, error) {
	result := make(map[string][]string)
	known := make(map[string]bool)
	for _, def := range doctor.BuiltinDefinitions() {
		known[def.Integration.ID] = true
	}

	for _, loc := range locations {
		parts := strings.SplitN(loc, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid location format %q, expected INTEGRATION=PATH", loc)
		}
		integ := strings.TrimSpace(parts[0])
		if !known[integ] {
			return nil, fmt.Errorf("unknown integration %q for location override", integ)
		}
		path := strings.TrimSpace(parts[1])
		result[integ] = append(result[integ], path)
	}
	return result, nil
}

func parseLaunchOptions(cmd *cli.Command) (LaunchOptions, error) {
	db := cmd.String("db")
	if db != "" && !filepath.IsAbs(db) {
		return LaunchOptions{}, fmt.Errorf("database path must be absolute: %q", db)
	}

	locs, err := validateLocations(cmd.StringSlice("location"))
	if err != nil {
		return LaunchOptions{}, err
	}

	days := cmd.Int("history-days")
	limit := cmd.Int("history-limit")
	stepBytes := cmd.Int("history-step-bytes")
	actionBytes := cmd.Int("history-action-bytes")
	metaBytes := cmd.Int("history-metadata-bytes")

	if days <= 0 || limit <= 0 || stepBytes <= 0 || actionBytes <= 0 || metaBytes <= 0 {
		return LaunchOptions{}, errors.New("history retention limits must be positive")
	}
	if stepBytes > actionBytes {
		return LaunchOptions{}, fmt.Errorf("history step bytes cannot exceed action bytes (%d > %d)", stepBytes, actionBytes)
	}

	pol := store.Policy{
		MaxAge:          time.Duration(days) * 24 * time.Hour,
		MaxTerminal:     limit,
		MaxStepOutput:   stepBytes,
		MaxActionOutput: actionBytes,
		MaxMetadata:     metaBytes,
	}

	return LaunchOptions{
		DBPath:        db,
		ProjectDir:    cmd.String("project"),
		Locations:     locs,
		SkillRoots:    cmd.StringSlice("skill-root"),
		HistoryPolicy: pol,
	}, nil
}

func newRootCommand(start func(context.Context, LaunchOptions) error, interactive func() bool) *cli.Command {
	return &cli.Command{
		Name:    "workstation-doctor",
		Usage:   "Workstation health inspector and safe maintenance TUI",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "db",
				Usage: "Explicit absolute path for action history database",
			},
			&cli.StringFlag{
				Name:  "project",
				Usage: "Project directory path for scoped inspection",
			},
			&cli.StringSliceFlag{
				Name:  "location",
				Usage: "Explicit location override in INTEGRATION=PATH format (repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  "skill-root",
				Usage: "Additional skill root directory path (repeatable)",
			},
			&cli.IntFlag{
				Name:  "history-days",
				Usage: "Retention maximum age in days for terminal actions",
				Value: 90,
			},
			&cli.IntFlag{
				Name:  "history-limit",
				Usage: "Maximum number of terminal actions to retain",
				Value: 10000,
			},
			&cli.IntFlag{
				Name:  "history-step-bytes",
				Usage: "Safe step output capture byte limit",
				Value: 16384,
			},
			&cli.IntFlag{
				Name:  "history-action-bytes",
				Usage: "Safe action total output capture byte limit",
				Value: 65536,
			},
			&cli.IntFlag{
				Name:  "history-metadata-bytes",
				Usage: "Safe action metadata byte limit",
				Value: 8192,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if !interactive() {
				fmt.Fprintln(cmd.Root().ErrWriter, "An interactive terminal is required.")
				return errors.New("An interactive terminal is required.") //nolint:revive,staticcheck // exact specification error string
			}
			opts, err := parseLaunchOptions(cmd)
			if err != nil {
				return err
			}
			return start(ctx, opts)
		},
	}
}

func runApplication(ctx context.Context, opts LaunchOptions) error {
	limits := doctor.DefaultLimits()
	engine, err := doctor.NewAuditEngine(doctor.BuiltinDefinitions(), limits)
	if err != nil {
		return fmt.Errorf("create audit engine: %w", err)
	}

	scope := doctor.Scope{
		ProjectDir: opts.ProjectDir,
		Locations:  opts.Locations,
		SkillRoots: opts.SkillRoots,
	}

	host, err := doctor.NewHost(scope, limits)
	if err != nil {
		return fmt.Errorf("create host: %w", err)
	}

	dbPath := opts.DBPath
	stateDir := defaultStateDir()
	if dbPath == "" {
		dbPath = filepath.Join(stateDir, "history.db")
	}

	service := app.NewService(engine, host, scope, app.Options{
		Version:  version,
		DBPath:   dbPath,
		StateDir: stateDir,
		Policy:   opts.HistoryPolicy,
	})

	return runTUI(ctx, service)
}

func main() {
	cmd := newRootCommand(runApplication, isTerminal)
	cmd.Root().ErrWriter = os.Stderr
	cmd.Root().Writer = os.Stdout
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}
