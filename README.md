# ♛ Diadem

A console DLNA media server. Run it in a folder of media files and every
DLNA client on your network — VLC on the TV in the living room, on a
laptop, on a phone — discovers it, browses the folder tree and streams the
files. No config, no database, no daemon: one binary, one folder.

```
♛ Diadem  ~/Movies · http://192.168.1.5:8200
3 folders · 42 items · 128.4 GB
──────────────────────────────────────────────
▾ Movies
  ▾ Sci-Fi
    🎬 Interstellar (2014).mkv                     12.8 GB
  ▾ Comedy
    🎬 Big Buck Bunny.mkv                           1.0 GB
──────────────────────────────────────────────
↑/↓ move · ⏎ open · tab next · r rescan · q quit
```

## Quick start

```
cd ~/Movies
diadem
```

Open VLC → **Local Network → Universal Plug'n'Play** → pick **Diadem**.
That's it.

## Install

From source (a Go toolchain is all you need):

```
git clone https://github.com/pandaCaffeine/diadem
cd diadem
go build -o /usr/local/bin/diadem .
```

Or straight from GitHub once published:

```
go install github.com/pandaCaffeine/diadem@latest
```

## Usage

```
diadem [--path FOLDER] [--name NAME] [--port PORT] [--debug]
```

| Flag | Default | What it does |
|---|---|---|
| `--path` | current folder | the folder to share |
| `--name` | `Diadem` | the server name clients display |
| `--port` | `8200` | HTTP port for browsing and streaming |
| `--debug` | off | write a verbose log to `diadem.log` in the temp folder |

Files are served **as they are** — no transcoding, no re-encoding. Anything
VLC can decode, Diadem can serve: video (`mkv`, `mp4`, `avi`, …), audio
(`flac`, `mp3`, …) and photos (`jpg`, `heic`, …). Hidden files stay hidden,
folders without media don't show up, and new files appear on their own
while the server runs — no restart needed.

### Keys

| Key | Action |
|---|---|
| `↑/↓`, `j/k`, `PgUp/PgDn` | move around the tree |
| `Enter` | collapse a folder / show file details (size, kind, direct URL) |
| `Tab` / `Shift+Tab` | switch tabs: **Library → Clients → Logs** |
| `r` | rescan the library |
| `q` | quit |

The **Clients** tab shows who is talking to the server right now (searching,
browsing, streaming), the **Logs** tab shows recent activity — the same
lines `--debug` writes to file.

## First run on macOS

The system will ask whether Diadem may accept incoming network connections
— choose **Allow**. That permission covers the SSDP discovery (UDP
multicast) and the media streams. If clients can't see the server at all,
check that the TV and the computer are on the same network: guest Wi-Fi and
"client isolation" on routers hide devices from each other.

## Troubleshooting

- **The TV doesn't list Diadem.** Same network? Firewall allowed? Quit and
  restart Diadem (it re-announces itself), then reopen the UPnP section in
  the client — most only refresh on reopen.
- **Port busy.** Another media server (minidlna, another Diadem) may hold
  port 8200 — run `diadem --port 8201`.
- **Seeking is slow on the TV, playback is fine.** Known quirk of some TV
  clients with byte-range requests on large files; laptops are unaffected.
- **"What did the TV actually ask for?"** Run with `--debug`, open the Logs
  tab or the log file, and watch SSDP/SOAP/HTTP lines scroll by.

## Development

```
go test ./...
go vet ./...
```

The layout follows the plan in [PLAN.md](PLAN.md): `internal/library`
(scanning, watching), `internal/httpd` (file serving with byte ranges),
`internal/dlna` (UPnP, SOAP ContentDirectory, SSDP), `internal/clients`
(activity tracking), `internal/tui` (the interface), `internal/logx`
(logging). Domain terms live in [CONTEXT.md](CONTEXT.md), decisions in
[docs/adr/](docs/adr/).
