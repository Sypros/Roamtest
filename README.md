# RoamTest

Walk-test Wi-Fi roaming for VoIP. The phone simulates a call (one packet every
20 ms) to a laptop on the wired LAN, records gaps, loss, latency and the access
point every second, and uploads each walk to a dashboard on the laptop that
ranks and charts all runs.

## Download

Everything is on the [latest release](../../releases/tag/latest), rebuilt on every change:

| File | Where |
| --- | --- |
| `RoamTest.apk` | Android phone. Open the link on the phone, install, allow "install unknown apps" once. |
| `RoamTest.exe` | Windows laptop, wired into the client's LAN. Its own window; allow it through the firewall once. |
| `RoamTest-Mac` | Same for macOS (opens in the browser). |
| `roamtest-termux` | The old command-line phone tool for Termux. |

## Use

1. Start RoamTest on the laptop. Pick or create a project (one per client/site).
2. Open RoamTest on the phone. It finds the laptop by itself (or tap **Find**).
3. Type what you changed as the run name, tap **START**, walk your route.
4. Tap **MARK SPOT** at doors, rooms or problem places; the marks show on the chart.
   The notification has Mark and Stop buttons too, so the phone can stay in your pocket.
5. Tap **STOP**. The run uploads; if the laptop is unreachable it is kept on the
   phone until you tap **Upload saved runs**.

The laptop app refreshes by itself, selects each new walk as it arrives, and
**Export** saves a zip per project to Downloads: `report.html` (opens on any
computer, no install), `runs.csv` (Excel) and the raw runs.

All data lives in one folder, `%LOCALAPPDATA%\RoamTest` on Windows; the exe
itself can sit anywhere and creates nothing next to it. Runs from the old
`roamtest-runs` folder are imported once into a project "Imported runs".

Ports: UDP 5005 (test traffic), UDP 5006 (discovery), TCP 8080 (dashboard and upload).

## Layout

- `android/` phone app (plain Java, no dependencies)
- `server/` laptop app and dashboard (Go; WebView2 window on Windows)
- `assets/` shared icon; `tools/winres` regenerates the Windows icon resource
- `cli/` Termux command-line tool (Go)
