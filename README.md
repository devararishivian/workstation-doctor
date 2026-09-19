# workstation-doctor

CLI audit untuk workstation Pi / Herdr / Ghostty / MCP / skills, dengan riwayat pengecekan dan aksi di SQLite embedded.

Stack: **Go 1.27** + `github.com/urfave/cli/v3` + `modernc.org/sqlite` (pure-Go, tanpa cgo). Tanpa GUI, tanpa dependensi
runtime baru.

## Build & pakai

```sh
go build -o workstation-doctor .
./workstation-doctor                 # menu interaktif
./workstation-doctor check           # 1. pengecekan read-only + simpan run
./workstation-doctor manual          # 2. langkah manual berurutan
./workstation-doctor fix             # 3. perbaikan otomatis (minta konfirmasi)
./workstation-doctor fix --yes       # otomatis tanpa konfirmasi
./workstation-doctor history         # riwayat run
./workstation-doctor history --run 3 # detail satu run + aksinya
./workstation-doctor --db /tmp/x.db check  # DB kustom
```

Exit code: `0` semua OK, `1` ada yang perlu update / aksi gagal, `2` ada cek UNKNOWN / DB tidak dapat dibuka.

## Cakupan cek

Versi terpasang vs terbaru: Pi, Herdr, Ghostty, opencode-ai, tokenjuice, serena-agent, gortex. Status kolektif:
`brew outdated`, `npm outdated -g`, paket `~/.pi/agent/npm`, git superpowers (`ls-remote`, read-only),
`herdr integration status`, validasi config Ghostty, validasi `settings.json` / `mcp.json` + cache MCP, dan audit batas
deskripsi skill 1024 char.

`check` tidak mengubah sistem. Satu-satunya akses jaringan adalah membaca versi terbaru (npm registry, brew info, PyPI,
git ls-remote). Isi `mcp.json` tidak pernah dicetak (mungkin berisi secret).

## Database riwayat

Bawaan: `~/.local/share/workstation-doctor/doctor.db` (flag `--db` untuk override). Skema v1:

- `check_runs` — satu baris per eksekusi (waktu, ringkasan OK/UPDATE/UNKNOWN, exit code)
- `check_results` — hasil per komponen per run
- `actions` — tiap perintah `fix` yang dijalankan (perintah, status ok/fail, output, waktu)

Bila DB tidak dapat dibuka, tool tetap jalan tanpa persistensi (peringatan ke stderr).

## Test & lint

```sh
go vet ./... && go test ./...
golangci-lint run ./...       # harus 0 issues (config: .golangci.yml)
golangci-lint run --fix ./... # perbaiki otomatis yang bisa
```
