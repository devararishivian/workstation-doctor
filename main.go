// Command workstation-doctor audits workstation components
// (Pi, Herdr, Ghostty, MCP, skills) and stores the history
// of checks and actions in embedded SQLite.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"

	// The SQLite driver registers at the application root so the
	// init() side effect stays visible here, not hidden in a library.
	_ "modernc.org/sqlite"
)

// Version is set at build time when necessary (-ldflags "-X main.version=...").
var version = "0.1.0"

// newOutputLogger builds a logger for program output. It writes plain
// lines without level or timestamp, so stdout stays pipeable for scripts.
func newOutputLogger(w io.Writer) zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{
		Out:          w,
		NoColor:      true,
		PartsExclude: []string{zerolog.TimestampFieldName, zerolog.LevelFieldName},
	})
}

// diag carries diagnostics (warnings and prompts) to stderr as leveled
// events. The level stays visible, the timestamp does not: for a CLI,
// when an event happened matters less than which run produced it.
var diag = zerolog.New(zerolog.ConsoleWriter{
	Out:          os.Stderr,
	NoColor:      true,
	PartsExclude: []string{zerolog.TimestampFieldName},
})

// defaultDBPath returns the default history database path. It returns
// an empty string when the home directory cannot be determined, so the
// flag stays unset and dbPath reports the problem with context.
func defaultDBPath() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".local", "share", "workstation-doctor", "doctor.db")
}

func dbPath(cmd *cli.Command) (string, error) {
	if v := cmd.String("db"); v != "" {
		return v, nil
	}
	return "", errors.New("cannot determine home directory for the default database path (set --db)")
}

// openStore opens the database. It returns nil when the database
// does not open, so checks still run without history (degraded mode).
func openStore(path string) *store.Store {
	st, err := store.Open(path)
	if err != nil {
		diag.Warn().Err(err).Msg("run history was not saved")
		return nil
	}
	return st
}

func toRows(results []doctor.Result) []store.ResultRow {
	rows := make([]store.ResultRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, store.ResultRow{
			Component: r.Component, Installed: r.Installed,
			Latest: r.Latest, Status: r.Status, Note: r.Note,
		})
	}
	return rows
}

// runAndRecord runs the checks and stores them as one run. It returns
// an error only when the database path cannot be determined; a storage
// failure degrades to a stderr warning so the check results survive.
func runAndRecord(ctx context.Context, cmd *cli.Command) ([]doctor.Result, int64, error) {
	path, err := dbPath(cmd)
	if err != nil {
		return nil, 0, err
	}
	start := time.Now()
	results := doctor.Run(ctx)
	s := doctor.Summarize(results)
	exit := 0
	if s.Update > 0 {
		exit = 1
	} else if s.Unknown > 0 {
		exit = 2
	}
	var runID int64
	if st := openStore(path); st != nil {
		id, err := st.RecordRun(ctx, start, time.Now(), s.OK, s.Update, s.Unknown, exit, toRows(results))
		if cerr := st.Close(); cerr != nil {
			diag.Warn().Err(cerr).Msg("failed to close history database")
		}
		if err != nil {
			diag.Warn().Err(err).Msg("failed to save run")
		} else {
			runID = id
		}
	}
	return results, runID, nil
}

func doCheck(ctx context.Context, cmd *cli.Command) error {
	results, runID, err := runAndRecord(ctx, cmd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	return renderCheck(newOutputLogger(cmd.Root().Writer), results, runID)
}

// renderCheck prints a gathered check result and maps it to an exit
// decision. Menu and subcommand paths share it.
func renderCheck(out zerolog.Logger, results []doctor.Result, runID int64) error {
	out.Info().Msg(doctor.FormatTable(results))
	if runID > 0 {
		out.Info().Msg("Saved run: #" + strconv.FormatInt(runID, 10))
	}
	s := doctor.Summarize(results)
	switch {
	case s.Update > 0:
		return cli.Exit(fmt.Sprintf("%d components need update", s.Update), 1)
	case s.Unknown > 0:
		return cli.Exit(fmt.Sprintf("%d checks returned UNKNOWN", s.Unknown), 2)
	default:
		return nil
	}
}

func doManual(ctx context.Context, cmd *cli.Command) error {
	results, runID, err := runAndRecord(ctx, cmd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	return renderManual(newOutputLogger(cmd.Root().Writer), results, runID)
}

// renderManual prints a gathered manual result. Menu and subcommand
// paths share it.
func renderManual(out zerolog.Logger, results []doctor.Result, runID int64) error {
	out.Info().Msg(doctor.FormatTable(results))
	out.Info().Msg(doctor.FormatManual(results))
	if runID > 0 {
		out.Info().Msg("Saved run: #" + strconv.FormatInt(runID, 10))
	}
	if doctor.Summarize(results).Update > 0 {
		return cli.Exit("updates are pending", 1)
	}
	return nil
}

func askConfirm(prompt string) bool {
	diag.Info().Msg(prompt + " [y/N]")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}

func doFix(ctx context.Context, cmd *cli.Command, autoYes bool) error {
	if err := legacyMaintenanceError(); err != nil {
		return cli.Exit(legacyMaintenanceMessage, 2)
	}
	results, runID, err := runAndRecord(ctx, cmd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	path, err := dbPath(cmd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	return runFixFlow(ctx, newOutputLogger(cmd.Root().Writer), path, results, runID, autoYes)
}

// runFixFlow executes the interactive part of fix on gathered results:
// list, confirm, execute, record. Menu and subcommand paths share it.
func runFixFlow(ctx context.Context, out zerolog.Logger, path string, results []doctor.Result, runID int64, autoYes bool) error {
	if err := legacyMaintenanceError(); err != nil {
		return cli.Exit(legacyMaintenanceMessage, 2)
	}
	out.Info().Msg(doctor.FormatTable(results))
	pending := doctor.Pending(results)
	out.Info().Msg("== Automatic fix ==")
	if len(pending) == 0 {
		out.Info().Msg("No action is needed. All components are current.")
		return nil
	}
	for i, r := range pending {
		out.Info().Msg(strconv.Itoa(i+1) + ". " + r.Fix)
	}
	if !autoYes && !askConfirm(fmt.Sprintf("Run the %d commands above in order?", len(pending))) {
		out.Info().Msg("Cancelled.")
		return nil
	}
	st := openStore(path)
	if st != nil {
		defer st.Close() //nolint:errcheck // close errors need no action when a command ends
	}
	fail := 0
	for _, r := range pending {
		out.Info().Msg("$ " + r.Fix)
		start := time.Now()
		c, cancel := context.WithTimeout(ctx, 10*time.Minute)
		raw, err := exec.CommandContext(c, "/bin/sh", "-c", r.Fix).CombinedOutput()
		cancel()
		end := time.Now()
		status := "ok"
		if err != nil {
			status = "fail"
			fail++
		}
		out.Info().Msg(truncateOut(string(raw)) + statusLine(status, r.Fix))
		if st != nil {
			if recErr := st.RecordAction(ctx, runID, "fix", r.Fix, status, lastBytes(string(raw), 4096), start, end); recErr != nil {
				diag.Warn().Err(recErr).Msg("failed to record action")
			}
		}
	}
	out.Info().Msg("Done. Failed: " + strconv.Itoa(fail) + " of " + strconv.Itoa(len(pending)) + ".")
	if fail > 0 {
		return cli.Exit(fmt.Sprintf("%d actions failed", fail), 1)
	}
	return nil
}

func statusLine(status, cmd string) string {
	if status == "ok" {
		return "OK: " + cmd
	}
	return "FAILED (continue): " + cmd
}

func truncateOut(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 20 {
		lines = append([]string{fmt.Sprintf("[... %d lines trimmed ...]", len(lines)-20)}, lines[len(lines)-20:]...)
	}
	return strings.Join(lines, "\n") + "\n"
}

func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func doHistory(ctx context.Context, cmd *cli.Command) error {
	out := newOutputLogger(cmd.Root().Writer)
	path, err := dbPath(cmd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	st := openStore(path)
	if st == nil {
		return cli.Exit("history database did not open", 2)
	}
	defer st.Close() //nolint:errcheck // close errors need no action when a command ends
	if id := cmd.Int64("run"); id > 0 {
		return showRun(ctx, out, st, id)
	}
	// Flags --limit and --run exist only on the history subcommand.
	// They read as zero from the interactive menu (root context).
	limit := cmd.Int("limit")
	if limit <= 0 {
		limit = 10
	}
	runs, err := st.ListRuns(ctx, limit)
	if err != nil {
		return cli.Exit(fmt.Sprintf("cannot read history: %v", err), 2)
	}
	if len(runs) == 0 {
		out.Info().Msg("No history yet. Run `check` first.")
		return nil
	}
	out.Info().Msg("\nRUN    STARTED              OK   UPDATE  UNKNOWN  EXIT")
	out.Info().Msg("----------------------------------------------------------------")
	for _, r := range runs {
		out.Info().Msg(fmt.Sprintf("#%-5d %-20s %-4d %-7d %-8d %d",
			r.ID, shortTS(r.StartedAt), r.NOk, r.NUpdate, r.NUnknown, r.ExitCode))
	}
	out.Info().Msg("Detail of one run: workstation-doctor history --run <id>")
	return nil
}

func showRun(ctx context.Context, out zerolog.Logger, st *store.Store, id int64) error {
	results, err := st.RunResults(ctx, id)
	if err != nil {
		return cli.Exit(fmt.Sprintf("cannot read run #%d: %v", id, err), 2)
	}
	if len(results) == 0 {
		return cli.Exit(fmt.Sprintf("run #%d was not found", id), 2)
	}
	out.Info().Msg("== Run #" + strconv.FormatInt(id, 10) + " ==")
	out.Info().Msg(doctor.FormatTable(toDoctor(results)))
	actions, err := st.RunActions(ctx, id)
	if err != nil {
		return cli.Exit(fmt.Sprintf("cannot read actions: %v", err), 2)
	}
	if len(actions) > 0 {
		out.Info().Msg("== Actions ==")
		for _, a := range actions {
			out.Info().Msg("- [" + a.Status + "] " + a.Kind + ": " + a.Command +
				" (" + shortTS(a.StartedAt) + " → " + shortTS(a.Finished) + ")")
		}
	}
	return nil
}

func toDoctor(rows []store.ResultRow) []doctor.Result {
	out := make([]doctor.Result, 0, len(rows))
	for _, r := range rows {
		out = append(out, doctor.Result{
			Component: r.Component, Installed: r.Installed,
			Latest: r.Latest, Status: r.Status, Note: r.Note,
		})
	}
	return out
}

func shortTS(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// menuRun runs a menu selection and reports failures. Exit code 1 only
// means updates are pending (the table already shows them), so only
// louder failures reach stderr and the menu stays in its loop.
func menuRun(err error) {
	if err == nil {
		return
	}
	var ec cli.ExitCoder
	if errors.As(err, &ec) && ec.ExitCode() == 1 {
		return
	}
	diag.Error().Msg(err.Error())
}

func interactiveMenu(ctx context.Context, cmd *cli.Command) error {
	return runMenu(ctx, cmd)
}

func main() {
	root := &cli.Command{
		Name:    "workstation-doctor",
		Usage:   "audit Pi / Herdr / Ghostty / MCP / skills + SQLite history",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "db",
				Usage: "SQLite history database file path",
				Value: defaultDBPath(),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() > 0 {
				return cli.ShowCommandHelp(ctx, cmd, cmd.Args().First())
			}
			return interactiveMenu(ctx, cmd)
		},
		Commands: []*cli.Command{
			{
				Name:   "check",
				Usage:  "1. read-only check + save run",
				Action: doCheck,
			},
			{
				Name:   "manual",
				Usage:  "2. show ordered manual steps",
				Action: doManual,
			},
			{
				Name:  "fix",
				Usage: "3. run automatic fix",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return doFix(ctx, cmd, cmd.Bool("yes"))
				},
			},
			{
				Name:  "history",
				Usage: "show check and action history",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 10, Usage: "number of runs to show"},
					&cli.Int64Flag{Name: "run", Usage: "show detail of one run"},
				},
				Action: doHistory,
			},
		},
	}
	if err := root.Run(context.Background(), os.Args); err != nil {
		cli.HandleExitCoder(err)
	}
}
