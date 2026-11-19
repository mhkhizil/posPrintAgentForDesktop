# Install and roll-out

## 1. Before you start

| Check | Why |
| --- | --- |
| Windows 10/11 x64 | The agent uses the Windows spooler and registry. |
| The printer's driver is installed | A USB printer with no queue cannot be printed to by anything. |
| The printer is on a fixed USB port and not a hub | Stops Windows re-enumerating it; the agent copes, but fixed is better. |
| Bluetooth printers are paired in Windows | Only then do they appear as a `COM` port. |
| The POS page origin is known | e.g. `https://pos.yourvenue.com` or `http://localhost:3000` in dev. |

## 2. Install

On each POS PC, from an **elevated** prompt:

```powershell
.\lnkits-print-agent-setup.exe
```

or, without the installer:

```powershell
.\pos-print-agent.exe --install
```

The service is created as `LnkitsPrintAgent`:

- Start type: Automatic
- Account: **LocalSystem** (needed for RAW spooler jobs and queue cleanup)
- Recovery: restart after 5 s, twice
- Config: `%PROGRAMDATA%\LnkitsPrintAgent\config.json`
- Log: `%PROGRAMDATA%\LnkitsPrintAgent\agent.log`

Verify:

```powershell
sc.exe query LnkitsPrintAgent
curl.exe http://127.0.0.1:8735/v1/health
```

The POS **Settings → Printer** should now show “Desktop direct printing ready”
instead of the QZ Tray requirement, and the printer list should fill itself in.

## 3. Lock down the origin

A fresh install accepts any origin (`allowOrigins: ["*"]`) so it works out of the
box. Before roll-out, set the POS origin:

```json
"allowOrigins": ["https://pos.yourvenue.com"]
```

then restart the service:

```powershell
Restart-Service LnkitsPrintAgent
```

A page from another origin is refused with `403 ORIGIN_NOT_ALLOWED`.

## 4. Printer setup in the POS app

1. Settings → Printer → **Connection: Installed printer (USB)**.
2. The list is live; there is no Discover button to press.
3. Pick the printer. Its stable identity is saved, not just its queue name.
4. **Test print**.

If the printer is moved to another USB port later, printing continues. The agent
finds the queue by identity and reports `rebound`, which the POS app saves
silently.

## 5. Clean-up of stale queues

Settings → Printer → **Clean up stale printers** (or
`POST /v1/admin/cleanup`). The service deletes queues whose printer is not
attached. This replaces the “run `reset-usb-printer.ps1` first” step.

Phantom `USBPRINT` devices that remain after a queue is deleted are handled by
Windows once the queue is gone; if one still shows in Device Manager, removing it
needs an elevated `pnputil /remove-device <instance>`.

## 6. Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| POS says “print agent is not running” | Service stopped or port different | `sc.exe query LnkitsPrintAgent`; `curl.exe http://127.0.0.1:8735/v1/health` |
| `401 UNAUTHORIZED` | Page cached an old token | Reload the POS page |
| `403 ORIGIN_NOT_ALLOWED` | POS origin missing from `allowOrigins` | Add it, restart the service |
| `403 NOT_ELEVATED` on cleanup | Agent running in the foreground | Use the service, or run the console elevated |
| `PRINTER_NOT_FOUND` (USB) | Driver not installed, or queue removed | Install the driver, then reopen Settings → Printer |
| `PRINTER_ACCESS_DENIED` | The queue belongs to another user/session | Run the service as LocalSystem (the default) |
| Nothing prints, no error | Wrong queue (e.g. a PDF printer) | Pick the queue whose port is `USB…` |
| Bluetooth printer missing | Not paired, or BLE-only | Pair it in Windows; BLE-only printers are out of scope in v1 |
| Printer list is empty | The Print Spooler service is stopped, or PowerShell is in Constrained Language mode | `Get-Service Spooler`; `Get-Printer` must list the printer in PowerShell |

Logs are text and rotate at 5 MB, three files:

```powershell
Get-Content "$env:PROGRAMDATA\LnkitsPrintAgent\agent.log" -Tail 100
```

## 7. Update

```powershell
# stop, replace the exe, install again (idempotent), start
Stop-Service LnkitsPrintAgent
Copy-Item .\pos-print-agent.exe 'C:\Program Files\Lnkits Print Agent\' -Force
.\pos-print-agent.exe --install
```

`--install` keeps an existing service registration and just (re)starts it.

## 8. Uninstall

```powershell
.\pos-print-agent.exe --uninstall
```

Configuration and logs under `%PROGRAMDATA%\LnkitsPrintAgent` are kept; delete the
folder to remove the token as well.
