# Native remote viewer (Windows)

`cmd/ourway-viewer` is the technician-side app for remote sessions. It adds
what the in-browser viewer cannot do: Ctrl+Alt+Del, multi-monitor selection,
text clipboard sync and tile-diff streaming.

## Flow

1. **Open in Viewer** on a device page calls `POST /api/devices/:id/sessions`,
   which returns `viewer_url` (`ourway://session/<id>?server=…&token=…&device=…`).
2. The browser opens that URL; Windows starts `ourway-viewer.exe` (registered per
   user in `HKCU\Software\Classes\ourway`; the viewer re-registers itself on every
   start, or run `ourway-viewer.exe --register` / `--unregister`).
3. The viewer connects to `/ws` with subprotocols `ourway-auth, <viewer token>, viewer`.
   The token is scoped to that one session. Closing the viewer ends the session.

## Wire protocol (viewer ⇄ server ⇄ remote exe)

Frames (remote exe → viewer, binary WebSocket messages; see `server/ws/viewer.go`):

| kind | layout |
|------|--------|
| `0x01` full | `[0x01][monitor u8][JPEG]` |
| `0x02` tiles | `[0x02][monitor u8][w u16][h u16][n u16]` then n × `[x u16][y u16][len u32][JPEG]` |

Text messages are JSON `{type, payload}`:

- viewer → exe: `input`, `clipboard`, `monitor_select`, `session_quality`, `request_keyframe`
- viewer → agent service: `special_key` (`ctrl_alt_del` only) → `send_sas`
- exe → viewer: `monitor_list`, `clipboard`, `session_info`
- server → exe: `viewer_attach` / `viewer_detach` (the exe only streams framed
  frames while a viewer is attached; the browser viewer keeps the legacy JPEG path)

The server drops the oldest queued frame for a slow viewer and asks the exe for a
keyframe, because later tile updates would otherwise be diffs against a lost frame.

## Ctrl+Alt+Del

Only a SYSTEM service can raise the Secure Attention Sequence, so the **agent
service** (not the user-session exe) calls `SendSAS`. On first use it sets
`HKLM\…\Policies\System\SoftwareSASGeneration` to allow services (bit 1).

## Building

```
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go -C cmd/ourway-viewer build -ldflags "-s -w -H windowsgui" .
```

The UI uses [Gio](https://gioui.org), which needs no C toolchain on Windows.
The portable parts (launch URL parsing, frame decoding, key map, client,
clipboard sync) are unit-tested on any OS.
