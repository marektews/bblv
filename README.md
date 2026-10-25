# Bambu Live View (bblv)

Watch your Bambu Lab 3D printers in the browser: a 3D view of the layers
printed so far, progress, temperatures, AMS slots, HMS alerts with
descriptions, and the camera. One small server written in Go, no cloud
account needed, works on Windows and Linux (including a Raspberry Pi).

- **3D view** of the current print built from its G-code: printed layers,
  the layer in progress highlighted, the rest as a transparent preview; a
  layer slider; colouring by filament or by path type (walls, infill,
  supports, …).
- **Status**: state and print stage, progress, remaining time and finish
  time, nozzle and bed temperatures, fans, speed profile, AMS slots and the
  external spool, HMS alerts and error codes explained in English or Polish.
- **Camera** stream (A1 and P1 series; X1 and H2 with ffmpeg installed).
- **Several printers** on one dashboard.
- **Web settings**: printers found in the network are listed automatically;
  access codes can be imported from Bambu Studio.
- **Read only**: bblv never sends commands to the printer.

The printer keeps working with Bambu Handy and the cloud. **LAN Only Mode and
Developer Mode are not needed**: the printer still publishes its status on
the local network in cloud mode, which is all bblv reads.

## Supported printers

| Model | Status |
|---|---|
| A1 | tested |
| A1 mini, P1P, P1S | experimental: same protocol as the A1, not tested |
| X1, X1C, X1E | experimental; the camera needs `ffmpeg` in `PATH` |
| H2D, H2S | experimental; the camera needs `ffmpeg`; dual nozzles are not shown separately |

If you own an experimental model, a recording of a print helps a lot — see
[Recording a print](#recording-a-print).

## Install

Download the binary for your system from the releases page (once there are
releases), or [build it](#building). Then run:

```sh
bblv serve
```

and open <http://localhost:8080> (or `http://<server-ip>:8080` from another
device). Add your printers in **Settings**.

| Flag | Default |
|---|---|
| `-data` | `%ProgramData%\bblv` on Windows, `/var/lib/bblv` on Linux |
| `-listen` | `:8080` |

The data directory holds `config.json` (printers, password hash; readable
only by its owner), the geometry cache (at most 1 GB) and recordings.

### Run as a service

```sh
bblv service install -data /var/lib/bblv -listen :8080   # as root / administrator
bblv service start
bblv service status
bblv service stop
bblv service uninstall
```

This registers a Windows service or a systemd unit started at boot. On
Windows the service logs to `bblv.log` in the data directory; on Linux use
`journalctl -u bblv`.

## Configuration

Open **Settings** in the web UI.

1. **Printers found in the network** are listed with their name, model, IP
   address and serial number; click **Add**. Printers announce themselves
   about every 10 seconds on UDP port 2021, which bblv shares with Bambu
   Studio.
2. **Access Code**: in cloud mode the A1 shows it on the screen only after
   enabling LAN Only Mode. Bambu Studio already knows it, though: **Import from
   this computer** reads `BambuStudio.conf` when bblv runs on the computer with
   Bambu Studio, or upload that file (`%APPDATA%\BambuStudio\BambuStudio.conf`
   on Windows, `~/.config/BambuStudio/` on Linux,
   `~/Library/Application Support/BambuStudio/` on macOS). The file is not
   stored, and access codes are never sent back to the browser. A service
   running under a system account cannot see your Bambu Studio file; use the
   upload then.
3. **Password** (optional): without one, everyone in your network can view
   and change everything. For access from outside, use a VPN rather than
   exposing bblv to the internet.

The printer must be reachable from the server on TCP ports 8883 (MQTT),
990 (FTPS) and 6000 (camera of the A1/P1) or 322 (RTSP camera of the X1/H2).

## How the 3D view gets the G-code

When a print starts, bblv downloads the project from the printer's SD card
over FTPS (`cache/<name>.3mf`, which contains the sliced G-code and the plate
thumbnail), parses it once and caches the result; the plate number comes
from the `N_<name>.bbl` file the printer writes next to it. This is tested
with prints sent through the cloud to an A1. If the file cannot be found,
upload the `.gcode` or `.3mf` in the printer view.

## Recording a print

Recordings of the MQTT reports are the best way to add or fix support for a
printer model without owning it:

```sh
bblv record -printer <id>          # Ctrl+C to stop; writes <data>/recordings/<id>-<time>.jsonl
```

Serial numbers, AMS and spool identifiers and network addresses are removed
by default (`-raw` keeps them; do not share such files). Older recordings can
be cleaned with `bblv anonymize <file.jsonl>`. File names of your prints stay
in the recording. Please attach the file to an issue together with the
printer model and firmware version.

A recording can be played back as a virtual printer, which is also handy for
frontend work:

```sh
bblv serve -replay recording.jsonl -replay-model print.3mf -speed 10
bblv replay -speed 0 recording.jsonl      # status log only
```

## Building

Requirements: Go 1.24+, Node.js 20.19+ or 22.12+.

```sh
./build.sh          # Linux, macOS, Git Bash
.\build.ps1         # Windows PowerShell
```

Both build the frontend (`web/`, Vue 3 + Element Plus + Three.js) into
`internal/webui/dist/`, which is embedded in the binary, and then build
`dist/bblv-windows-amd64.exe`, `dist/bblv-linux-amd64` and
`dist/bblv-linux-arm64` (pure Go, `CGO_ENABLED=0`).

During development run `bblv serve` and, in `web/`, `npm run dev`; the Vite
dev server proxies `/api` to `localhost:8080`.

The HMS and error code descriptions are generated from the texts Bambu Lab
publishes for Bambu Studio:

```sh
go run ./internal/hms/gen     # refreshes internal/hms/data/*.json.gz
```

## Other commands

```sh
bblv geometry -file print.3mf      # parse a local file and print statistics
bblv geometry -printer <id>        # fetch and parse the current print
bblv version
```

## Acknowledgements

- [OpenBambuAPI](https://github.com/Doridian/OpenBambuAPI) and
  [ha-bambulab](https://github.com/greghesp/ha-bambulab) for documenting the
  printer protocols; the list of print stage numbers comes from ha-bambulab.
- HMS and error code descriptions are © Bambu Lab, from the service Bambu
  Studio uses.
- Icons: [Font Awesome Free](https://fontawesome.com) (icons CC BY 4.0, code
  MIT).

bblv is not affiliated with Bambu Lab.

## License

[MIT](LICENSE)
