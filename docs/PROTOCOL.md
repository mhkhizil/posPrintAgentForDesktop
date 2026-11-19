# HTTP contract (v1)

Base URL: `http://127.0.0.1:8735`

All responses are JSON. Print failures use
`{ "success": false, "code": "...", "message": "..." }` where `message` is written
for a shopkeeper, not for a developer.

## Authentication

`GET /v1/health` is open (a page must be able to discover the agent). Every other
endpoint requires:

```
X-Pos-Agent-Token: <token from /v1/health>
```

The token is generated once at install and stored in
`%PROGRAMDATA%\LnkitsPrintAgent\config.json`.

## CORS

Allowed origins come from `allowOrigins`. The agent echoes the origin (or `*`
when every origin is allowed), allows the `X-Pos-Agent-Token` and `Content-Type`
headers, and answers Chromium's Private Network Access preflight with
`Access-Control-Allow-Private-Network: true`.

## GET /v1/health

```json
{
  "ok": true,
  "agentVersion": "1.0.0",
  "contractVersion": 1,
  "platform": "windows",
  "elevated": true,
  "capabilities": ["NETWORK", "USB", "BLUETOOTH", "PRINTER_CLEANUP"],
  "allowOrigins": ["*"],
  "uptimeMs": 4210,
  "token": "9f2c…"
}
```

## GET /v1/printers?transport=USB

`transport` is `USB` (Windows print queues), `BLUETOOTH` (paired SPP ports) or
empty (returns an empty list). Network printers are addressed by IP and are not
enumerated.

```json
{
  "printers": [
    {
      "id": "usbprint:usbprint\\1fc9_2016\\a1b2c3",
      "kind": "USB",
      "name": "POS-80 (Copy 1)",
      "port": "USB002",
      "driver": "XPrinter XP-80C",
      "model": "XP-80C",
      "connected": true,
      "stale": false,
      "status": "idle"
    }
  ]
}
```

`id` is the stable identity. `name` and `port` are what Windows currently calls
the queue and may change when a printer moves port. `stale` marks a leftover
queue whose printer is offline (or marked work-offline).

## POST /v1/print

```json
{
  "jobId": "sale-123-copy-1",
  "transport": "USB",
  "printerId": "usbprint:usbprint\\1fc9_2016\\a1b2c3",
  "deviceId": "POS-80",
  "host": null,
  "port": null,
  "dataBase64": "G0A…",
  "encoding": "base64",
  "timeoutMs": 8000
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `transport` | yes | `NETWORK`, `USB`, `BLUETOOTH`. |
| `dataBase64` | yes | The ESC/POS byte stream, already encoded by the POS app. |
| `printerId` | USB | Preferred: the stable identity. |
| `deviceId` | USB/BT | Queue name or `COMn`; used when `printerId` is missing or stale. |
| `host`, `port` | NETWORK | `port` defaults to `9100`. |
| `jobId` | no | Echoed into the log for support. |
| `timeoutMs` | no | Clamped to 500–60000 ms. |

Resolution order for USB: **identity → queue name → port name → the single
connected printer**. If Windows renamed the queue, the job still prints and the
response says `rebound: true`.

Success:

```json
{
  "success": true,
  "bytesWritten": 2311,
  "via": "spooler",
  "durationMs": 42,
  "rebound": true,
  "printer": { "id": "usbprint:…", "name": "POS-80 (Copy 1)", "port": "USB002" }
}
```

Failure codes: `PRINTER_NOT_FOUND`, `PRINTER_BUSY`, `PRINTER_ACCESS_DENIED`,
`PRINTER_ERROR`, `UNSUPPORTED_TRANSPORT`, `UNSUPPORTED_ENCODING`,
`INVALID_DATA`, `EMPTY_JOB`, `JOB_TOO_LARGE`, `HOST_REQUIRED`, `INVALID_HOST`,
`PRINTER_REQUIRED`.

## POST /v1/probe

```json
{ "host": "192.168.1.50", "port": 9100 }
```

```json
{ "ok": true, "latencyMs": 12 }
```

## POST /v1/admin/cleanup

Removes print queues whose printer is no longer attached. Requires the agent to
run elevated (the service does); otherwise `403 NOT_ELEVATED`.

```json
{ "removedQueues": ["POS-80"], "failedQueues": [] }
```

## Versioning

`contractVersion` changes only on a breaking change. Additive fields are
introduced without a version bump; clients ignore unknown fields.
