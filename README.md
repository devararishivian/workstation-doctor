# workstation-doctor

CLI audit untuk workstation Pi / Herdr / Ghostty / MCP / skills.
Stack: **Bash + python3 stdlib**. Tanpa dependensi baru, tanpa GUI.

## Pakai

```sh
./workstation-doctor            # menu interaktif
./workstation-doctor --check    # 1. pengecekan read-only
./workstation-doctor --manual   # 2. langkah manual berurutan
./workstation-doctor --fix      # 3. perbaikan otomatis (minta konfirmasi)
./workstation-doctor --fix --yes # otomatis tanpa konfirmasi
```

Exit code: `0` = semua OK, `1` = ada yang perlu diupdate, `2` = ada cek UNKNOWN/gagal.

## Cakupan cek

Versi terpasang vs terbaru: Pi, Herdr, Ghostty, opencode-ai,
tokenjuice, serena-agent, gortex. Status kolektif: `brew outdated`,
`npm outdated -g`, paket `~/.pi/agent/npm`, git superpowers
(`ls-remote`, read-only), `herdr integration status`, validasi config
Ghostty, validasi `settings.json` / `mcp.json` + cache MCP, dan audit
batas deskripsi skill 1024 char.

`--check` tidak mengubah sistem. Satu-satunya akses jaringan adalah
membaca versi terbaru (npm registry, brew info, PyPI, git ls-remote).
Tidak ada secret yang dicetak (isi `mcp.json` tidak pernah di-dump).
