# Lnkits POS Print Agent

A small Windows service that prints ESC/POS receipts and kitchen tickets for the
Lnkits POS web app, without QZ Tray, without a print dialog, and without a
WebSocket certificate prompt.

The web app keeps running exactly as it does today (Docker, nginx, Vercel or a
local dev server). It only gains one optional extra: an HTTP call to
`http://127.0.0.1:8735`.

```
POS page (any Chromium/Edge tab) ──fetch──► 127.0.0.1:8735 (this service, LocalSystem)
                                                  │
                       ┌──────────────────────────┼──────────────────────────┐
                  Win32 spooler RAW          TCP host:9100            serial COMx
                  (USB printers)             (Wi-Fi printers)         (Bluetooth SPP)
```

## Why it exists

A browser cannot:

- open a raw TCP socket to a Wi-Fi printer on port 9100;
- print to a USB printer whose Windows driver (`usbprint.sys`) owns the device;
- write to a Bluetooth SPP COM port.

It also cannot enumerate print queues, so the POS app had to rely on QZ Tray and
on print-queue names containing words like `POS-80`. This service does all of it
natively and identifies a printer by its **device identity**, so a printer that
moves USB port keeps printing without anyone re-picking it in Settings.

## Requirements

- Windows 10/11 (x64). macOS/Linux are buildable for network + CUPS printing.
- Go 1.23 or newer to build. There are **no third-party dependencies**.
- Administrator rights once, to install the service.

## Build

```powershell
# from the repository root
.\build\build.ps1 -Version 1.0.0
```

Produces `dist\pos-print-agent.exe`. The script runs `go vet` and `go test`
first. To build by hand:

```powershell
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w -X main.version=1.0.0" -o dist\pos-print-agent.exe .\cmd\pos-print-agent
```

## Run in the foreground (development)

```powershell
.\dist\pos-print-agent.exe --listen 127.0.0.1:8735 --allow-origin http://localhost:3000
```

It creates `%PROGRAMDATA%\LnkitsPrintAgent\config.json` with a random token and
logs to `%PROGRAMDATA%\LnkitsPrintAgent\agent.log`.

Check it:

```powershell
curl.exe http://127.0.0.1:8735/v1/health
```

## Install as a service

From an **elevated** prompt:

```powershell
.\dist\pos-print-agent.exe --install
```

This creates `LnkitsPrintAgent` (Automatic, runs as **LocalSystem**, restart on
failure) and starts it. `--uninstall` removes it again.

LocalSystem is deliberate: only an elevated process can remove stale print
queues and keep printing when a printer is re-enumerated.

## Configuration

`%PROGRAMDATA%\LnkitsPrintAgent\config.json`

| Key | Default | Meaning |
| --- | --- | --- |
| `listen` | `127.0.0.1:8735` | Loopback address. Never bind a LAN address. |
| `allowOrigins` | `["*"]` | Browser origins allowed to call the agent. **Set this to your POS origin before rollout.** |
| `token` | generated | Shared secret sent as `X-Pos-Agent-Token`. |
| `logLevel` | `info` | `debug`, `info`, `warn`, `error`. |
| `logDir` | `%PROGRAMDATA%\LnkitsPrintAgent` | Rotating log location. |
| `maxBodyBytes` | `8388608` | Largest single ESC/POS job (8 MB). |
| `defaultTimeoutMs` | `8000` | Used when a job names no timeout. |

`allowOrigins` is permissive by default so a fresh install works; every other
endpoint still requires the token. Lock it down with:

```json
"allowOrigins": ["https://your-pos.example.com"]
```

## HTTP contract

See [docs/PROTOCOL.md](docs/PROTOCOL.md). Summary:

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/v1/health` | Version, capabilities, elevation, token |
| GET | `/v1/printers?transport=USB` | Installed USB queues with device identity |
| POST | `/v1/print` | Send base64 ESC/POS bytes |
| POST | `/v1/probe` | Test a network printer without printing |
| POST | `/v1/admin/cleanup` | Remove stale/offline print queues (elevated) |

## Security

- Binds loopback only (`127.0.0.1` and `::1`), never `0.0.0.0`.
- Every printing endpoint requires the token, compared in constant time.
- Origin allow-list, plus the Chromium Private Network Access preflight header.
- 8 MB body cap, per-printer serialisation, no shell interpolation of print data.
- The token is read by the page from `/v1/health`; it is never written to disk by
  the browser.

## Install notes

- A USB printer still needs its Windows driver installed once. The agent lists
  every queue it finds, so the queue name no longer has to contain a keyword.
- Bluetooth printers must be paired in Windows first; they then appear as a
  `COM` port under `transport=BLUETOOTH`.
- See [docs/INSTALL.md](docs/INSTALL.md) for the roll-out checklist and the
  troubleshooting table.
