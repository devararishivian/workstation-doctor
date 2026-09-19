// Command workstation-doctor mengaudit komponen workstation
// (Pi, Herdr, Ghostty, MCP, skills) dan menyimpan riwayat
// pengecekan serta aksi ke SQLite embedded.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"workstation-doctor/internal/doctor"
	"workstation-doctor/internal/store"
)

// Version diisi saat build bila perlu (-ldflags "-X main.version=...").
var version = "0.1.0"

func dbPath(cmd *cli.Command) string {
	if v := cmd.String("db"); v != "" {
		return v
	}
	return store.DefaultPath()
}

// openStore membuka DB; bila gagal, kembalikan nil agar cek tetap jalan
// tanpa persistensi (mode degradasi).
func openStore(path string) *store.Store {
	st, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "peringatan: riwayat tidak tersimpan (%v)\n", err)
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

// runAndRecord menjalankan cek dan menyimpannya sebagai satu run.
func runAndRecord(ctx context.Context, path string) ([]doctor.Result, int64) {
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
		id, err := st.RecordRun(start, time.Now(), s.OK, s.Update, s.Unknown, exit, toRows(results))
		_ = st.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "peringatan: gagal menyimpan run (%v)\n", err)
		} else {
			runID = id
		}
	}
	return results, runID
}

func doCheck(ctx context.Context, cmd *cli.Command) error {
	results, runID := runAndRecord(ctx, dbPath(cmd))
	doctor.PrintTable(cmd.Root().Writer, results)
	if runID > 0 {
		fmt.Fprintf(cmd.Root().Writer, "Run tersimpan: #%d\n", runID)
	}
	s := doctor.Summarize(results)
	switch {
	case s.Update > 0:
		return cli.Exit(fmt.Sprintf("%d komponen perlu update", s.Update), 1)
	case s.Unknown > 0:
		return cli.Exit(fmt.Sprintf("%d cek tidak dapat ditentukan", s.Unknown), 2)
	default:
		return nil
	}
}

func doManual(ctx context.Context, cmd *cli.Command) error {
	results, runID := runAndRecord(ctx, dbPath(cmd))
	doctor.PrintTable(cmd.Root().Writer, results)
	doctor.PrintManual(cmd.Root().Writer, results)
	if runID > 0 {
		fmt.Fprintf(cmd.Root().Writer, "Run tersimpan: #%d\n", runID)
	}
	if doctor.Summarize(results).Update > 0 {
		return cli.Exit("ada pembaruan tertunda", 1)
	}
	return nil
}

func askConfirm(prompt string) bool {
	fmt.Print(prompt + " [y/N] ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}

func doFix(ctx context.Context, cmd *cli.Command, autoYes bool) error {
	path := dbPath(cmd)
	results, runID := runAndRecord(ctx, path)
	w := cmd.Root().Writer
	doctor.PrintTable(w, results)
	pending := doctor.Pending(results)
	fmt.Fprintln(w, "\n== Perbaikan otomatis ==")
	if len(pending) == 0 {
		fmt.Fprintln(w, "Tidak ada tindakan. Semua komponen terkini.")
		return nil
	}
	for i, r := range pending {
		fmt.Fprintf(w, "%d. %s\n", i+1, r.Fix)
	}
	if !autoYes && !askConfirm(fmt.Sprintf("Jalankan %d perintah di atas berurutan?", len(pending))) {
		fmt.Fprintln(w, "Dibatalkan.")
		return nil
	}
	st := openStore(path)
	if st != nil {
		defer st.Close()
	}
	fail := 0
	for _, r := range pending {
		fmt.Fprintf(w, "\n$ %s\n", r.Fix)
		start := time.Now()
		c, cancel := context.WithTimeout(ctx, 10*time.Minute)
		out, err := exec.CommandContext(c, "/bin/sh", "-c", r.Fix).CombinedOutput()
		cancel()
		end := time.Now()
		status := "ok"
		if err != nil {
			status = "fail"
			fail++
		}
		fmt.Fprintf(w, "%s%s\n", truncateOut(string(out)), statusLine(status, r.Fix))
		if st != nil {
			if recErr := st.RecordAction(runID, "fix", r.Fix, status, lastBytes(string(out), 4096), start, end); recErr != nil {
				fmt.Fprintf(os.Stderr, "peringatan: gagal mencatat aksi (%v)\n", recErr)
			}
		}
	}
	fmt.Fprintf(w, "\nSelesai. Gagal: %d dari %d.\n", fail, len(pending))
	if fail > 0 {
		return cli.Exit(fmt.Sprintf("%d aksi gagal", fail), 1)
	}
	return nil
}

func statusLine(status, cmd string) string {
	if status == "ok" {
		return "OK: " + cmd
	}
	return "GAGAL (lanjut): " + cmd
}

func truncateOut(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 20 {
		lines = append([]string{fmt.Sprintf("[... %d baris dipangkas ...]", len(lines)-20)}, lines[len(lines)-20:]...)
	}
	return strings.Join(lines, "\n") + "\n"
}

func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func doHistory(_ context.Context, cmd *cli.Command) error {
	w := cmd.Root().Writer
	st := openStore(dbPath(cmd))
	if st == nil {
		return cli.Exit("database riwayat tidak dapat dibuka", 2)
	}
	defer st.Close()
	if id := cmd.Int64("run"); id > 0 {
		return showRun(w, st, id)
	}
	// Flag --limit/--run hanya terdefinisi di subcommand history;
	// bila dipanggil dari menu interaktif (konteks root) nilainya nol.
	limit := int(cmd.Int("limit"))
	if limit <= 0 {
		limit = 10
	}
	runs, err := st.ListRuns(limit)
	if err != nil {
		return cli.Exit(fmt.Sprintf("gagal baca riwayat: %v", err), 2)
	}
	if len(runs) == 0 {
		fmt.Fprintln(w, "Belum ada riwayat. Jalankan `check` terlebih dahulu.")
		return nil
	}
	fmt.Fprintf(w, "\n%-6s %-20s %-4s %-7s %-8s %s\n", "RUN", "MULAI", "OK", "UPDATE", "UNKNOWN", "EXIT")
	fmt.Fprintln(w, "----------------------------------------------------------------")
	for _, r := range runs {
		fmt.Fprintf(w, "#%-5d %-20s %-4d %-7d %-8d %d\n",
			r.ID, shortTS(r.StartedAt), r.NOk, r.NUpdate, r.NUnknown, r.ExitCode)
	}
	fmt.Fprintln(w, "\nDetail satu run: workstation-doctor history --run <id>")
	return nil
}

func showRun(w interface{ Write([]byte) (int, error) }, st *store.Store, id int64) error {
	results, err := st.RunResults(id)
	if err != nil {
		return cli.Exit(fmt.Sprintf("gagal baca run #%d: %v", id, err), 2)
	}
	if len(results) == 0 {
		return cli.Exit(fmt.Sprintf("run #%d tidak ditemukan", id), 2)
	}
	fmt.Fprintf(w, "\n== Run #%d ==\n", id)
	doctor.PrintTable(w, toDoctor(results))
	actions, err := st.RunActions(id)
	if err != nil {
		return cli.Exit(fmt.Sprintf("gagal baca aksi: %v", err), 2)
	}
	if len(actions) > 0 {
		fmt.Fprintf(w, "\n== Aksi ==\n")
		for _, a := range actions {
			fmt.Fprintf(w, "- [%s] %s: %s (%s → %s)\n",
				a.Status, a.Kind, a.Command, shortTS(a.StartedAt), shortTS(a.Finished))
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

func interactiveMenu(ctx context.Context, cmd *cli.Command) error {
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("\nworkstation-doctor")
		fmt.Println("  1. Cek status (read-only)")
		fmt.Println("  2. Tampilkan langkah manual berurutan")
		fmt.Println("  3. Jalankan perbaikan otomatis")
		fmt.Println("  4. Lihat riwayat")
		fmt.Println("  0. Keluar")
		fmt.Print("Pilih [0-4]: ")
		if !in.Scan() {
			return nil
		}
		switch strings.TrimSpace(in.Text()) {
		case "1":
			_ = doCheck(ctx, cmd)
		case "2":
			_ = doManual(ctx, cmd)
		case "3":
			_ = doFix(ctx, cmd, false)
		case "4":
			_ = doHistory(ctx, cmd)
		case "0", "q", "quit", "exit":
			return nil
		default:
			fmt.Println("Pilihan tidak dikenal.")
		}
	}
}

func main() {
	root := &cli.Command{
		Name:    "workstation-doctor",
		Usage:   "audit Pi / Herdr / Ghostty / MCP / skills + riwayat SQLite",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "db",
				Usage: "path file database riwayat SQLite",
				Value: store.DefaultPath(),
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
				Usage:  "1. pengecekan read-only + simpan run",
				Action: doCheck,
			},
			{
				Name:   "manual",
				Usage:  "2. tampilkan langkah manual berurutan",
				Action: doManual,
			},
			{
				Name:  "fix",
				Usage: "3. jalankan perbaikan otomatis",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "lewati konfirmasi"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return doFix(ctx, cmd, cmd.Bool("yes"))
				},
			},
			{
				Name:  "history",
				Usage: "lihat riwayat pengecekan dan aksi",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 10, Usage: "jumlah run ditampilkan"},
					&cli.Int64Flag{Name: "run", Usage: "tampilkan detail run tertentu"},
				},
				Action: doHistory,
			},
		},
	}
	if err := root.Run(context.Background(), os.Args); err != nil {
		cli.HandleExitCoder(err)
	}
}
