# Bambu Live View (bblv)

Watch your Bambu Lab 3D printers in the browser: a 3D view of the layers
printed so far, progress, temperatures, AMS slots, HMS alerts with
descriptions, and the camera. One small server written in Go, no cloud
account needed, works on Windows and Linux (including a Raspberry Pi).

- **3D view** of the current print built from its G-code: printed layers,
  the layer in progress highlighted, the rest as a transparent preview; a
  layer slider; colouring by filament or by path type (walls, infill,
  supports, …); a legend with the AMS slot, filament profile and planned
  weight of each filament.
- **Status**: state and print stage, progress, remaining time and finish
  time, nozzle and bed temperatures, fans, speed profile, the slicer
  profiles and planned filament of the print, AMS slots with spool details
  and the conditions inside the AMS, the external spool, the SD card, HMS
  alerts and error codes explained in English or Polish. See
  [Dashboard and printer view](#dashboard-and-printer-view).
- **Camera** stream (A1 and P1 series; X1, X2, H2 and P2 with ffmpeg installed),
  also in full screen; when it does not work, the panel says why. A button
  in its corner switches the printer's light, the only thing bblv changes
  on a printer; the A1 accepts it also in cloud mode, without LAN only or
  developer mode.
- **Several printers** on one dashboard, grouped by state.
- **Web settings**: printers found in the network are listed automatically;
  access codes can be imported from Bambu Studio.
- **Follows printers to a new address** when the DHCP server gives them
  another one; see [When a printer gets another
  address](#when-a-printer-gets-another-address).
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
| H2D, H2S | tested, except the camera, which needs `ffmpeg` and LAN live view enabled on the printer; dual nozzles are not shown separately |
| X2D, P2S | tested, except the camera, which needs `ffmpeg` and LAN live view enabled on the printer |
| H2D Pro, H2C, A2L | experimental: recognised in the network, not tested; the camera of the H2 series needs `ffmpeg` |

If you own an experimental model, a recording of a print helps a lot — see
[Recording a print](#recording-a-print).

## Install

Download the binary for your system from the [releases
page](https://github.com/marektews/bblv/releases), where `SHA256SUMS` lists
the checksums of the files, or [build it](#building). Then run:

```sh
bblv serve
```

and open <https://localhost:8443> (or `https://<server-ip>:8443` from another
device; `http://…:8080` redirects there). Add your printers in **Settings**.

bblv serves HTTPS with a self-signed certificate, which keeps the password
and the session cookie off the network in clear text. Browsers warn about
it, as nobody vouches for it: accept the warning once on every device (in
Chrome *Advanced → Proceed*). The certificate, `https-cert.pem` and
`https-key.pem` in the data directory, names `localhost`, the computer's
name and its IPv4 addresses. It is renewed, also while the server runs, a
month before it expires after about a year, or when the computer gets
another address in the local network, and the warning comes again then.
Addresses of virtual adapters (Hyper-V, WSL, Docker), which change at every
boot, do not renew it; to add them, delete both files and restart bblv.
`-https=` turns HTTPS off and serves plain HTTP on
`-listen`. On Windows, other devices reach the server only if the firewall
lets them in on port 8443 (and 8080 for the redirect).

| Flag | Default |
|---|---|
| `-data` | `%ProgramData%\bblv` on Windows, `/var/lib/bblv` on Linux |
| `-https` | `:8443`; empty: plain HTTP only |
| `-listen` | `:8080`, redirecting to HTTPS |
| `-allow-host` | none: comma-separated further host names, see below |

The web interface opens under an IP address, `localhost`, a computer name
without dots (`https://mypc:8443`) or a local name ending in `.local`,
`.lan`, `.home.arpa` or `.internal`. Other names are refused, which keeps
pages of other sites from reaching bblv through DNS rebinding; name your
own, e.g. that of a reverse proxy, with `-allow-host bblv.example.com`.

On start bblv restricts the data directory to its owner (on Windows also
to SYSTEM and the administrators), since other users of the computer
could otherwise read the access codes. It holds `config.json` (printers,
password hash), the geometry cache (at most 1 GB), recordings,
`filaments.json` (filament profile names learned from print files) and
`printer-ca.pem` (printer CA certificates learned from the printers). bblv
itself updates the address of a printer in `config.json` when the printer
moves; see [When a printer gets another
address](#when-a-printer-gets-another-address).

### Run as a service

```sh
bblv service install -data /var/lib/bblv -listen :8080   # as root / administrator
bblv service start
bblv service status
bblv service stop
bblv service uninstall
```

This registers a Windows service or a systemd unit started at boot. On
Windows the service logs to `bblv.log` in the data directory, up to 5 MB,
with the 5 MB before in `bblv.log.1`; on Linux use `journalctl -u bblv`. A
printer that cannot be reached is reported when it happens and then once an
hour. Stopping the service lets requests in progress finish and disconnects
the printers, which also ends ffmpeg processes of the cameras.

On Linux, `-user bblv` (with `install`) runs the service as that account
instead of root and makes it the owner of the data directory; create the
account first, e.g. `useradd --system bblv`. A Windows service runs as
LocalSystem.

The RTSP cameras (X1, X2, H2, P2) get the access code on the ffmpeg command
line. On Linux every local user can read command lines unless `/proc` is
mounted with `hidepid=2`; on Windows users without administrator rights
cannot read those of a service. ffmpeg does not check the printer's
certificate either.

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
   exposing bblv to the internet. After a wrong password further attempts,
   from any browser, wait 1 s, then 2, 4… up to 30 s. Logging out ends the
   session also for a copy of its cookie; changing the password ends all
   other sessions.

bblv recognises a printer by its certificate, on MQTT, FTPS and the camera
port 6000 alike: it names the printer's serial number and is signed by a
Bambu Lab CA. On a printer's first connection bblv learns that CA and pins
the printer to it in `printer-ca.pem`; later connections with a certificate
from another CA are refused, so a device taking over the printer's address
gets no access code. The view of the printer then says why it is offline.
For the same reason, changing a printer's serial number in **Settings**
needs its access code again, and removing a printer also forgets its pin
and a CA no other printer uses. If the certificates change for real, e.g. with new firmware, delete
`printer-ca.pem` and restart bblv. If the file cannot be read, bblv does
not learn the CAs again, which would trust whatever device answers first:
the printers stay offline, their view says why, and the file is left for
you to fix or delete. Expiry dates are not checked: the CA of the A1
expires in 2032, before the certificates it signed.

The printer table shows whether each printer is connected and has an SD
card. It can be sorted by name, IP address or serial number; the browser
remembers the order.

The printer must be reachable from the server on TCP ports 8883 (MQTT),
990 (FTPS) and 6000 (camera of the A1/P1) or 322 (RTSP camera of the X1/X2/H2/P2,
open only with LAN live view enabled on the printer).

### When a printer gets another address

If a printer has been offline for 2 minutes, bblv looks for it:

- at the address it announces with SSDP; the announcements do not cross
  routers, so this works only in the same subnet;
- by probing TCP port 8883 on every address of the /24 subnet of its last
  address. The certificate of the printer's MQTT broker has its serial
  number as the common name.

When found, the new address is saved to `config.json` and the printer is
reconnected. While it stays offline, its announcements are checked every
30 seconds and the subnet is searched again after 10 minutes, then at
doubling intervals up to every 2 hours, so that a printer switched off for
the night does not have the network scanned until the morning. An address
edited in **Settings** in the meantime is not overwritten.

A printer is trusted at a new address only when its certificate is also
signed by the CA it is pinned to (see [Configuration](#configuration)), so
no other device can obtain the access code by claiming a serial number.
Until a printer has connected once, it is not followed. For a printer configured by host name rather
than an IPv4 address, only the announced address is checked, not the
subnet.

## Dashboard and printer view

The dashboard shows a tile per printer, sorted by name and grouped by
state, with the states that need attention (failed, paused) first and
offline printers last. The **Group by state** switch in the top bar turns
the grouping off; the browser remembers the choice. A tile shows an alert
icon, in the colour of the most severe alert, when the printer reports HMS
alerts or an error code.

An alert that has been read can be removed with its × in the **Alerts**
section of the printer, or all at once with **Remove all**. It is hidden for
everyone, also from the tile, but nothing changes on the printer; when the
printer stops reporting it, bblv forgets it, so that it shows again if it
comes back. **Show hidden** lists the removed ones, which can be restored.
The removed alerts are kept in `dismissed-alerts.json` in the data
directory.

Besides temperatures, fans and alerts, the status panel of a printer shows:

- **Print**: the file and plate; the process profile, printer profile,
  layer height and the filament profile of each filament used, from the
  slicer settings in the G-code (the `(<project>.3mf)` suffixes Bambu Studio
  appends to project profiles are dropped); the filament the slicer planned,
  in grams and metres, from the `.3mf` (per filament,
  `Metadata/slice_info.config`) or the G-code header. The 3D view legend
  shows the filament profile and grams of each filament.
- **Filaments**: humidity, temperature and drying (time left, temperature)
  of each AMS unit; with several units they are named A–D, AMS HT units
  HT1…. On the A1 series the unit is labelled AMS lite with a warning icon:
  the AMS lite is open and has no humidity sensor, so the values the printer
  reports may be estimates or placeholders. Each slot shows the spool's type
  and filament profile name (see below), a mark for Bambu Lab spools with an
  RFID tag, the percent, grams and metres left, the nozzle temperature range
  and which filaments of the current print it feeds. That mapping comes from
  the `.bbl` file on the SD card or, on printers that keep no `.bbl` file
  (X2D, P2S, H2D, H2S), from the report (`print.mapping`).
- **Device**: Wi-Fi signal, firmware, whether the printer has an SD card
  and, for printers that report it, its free and total space; for printers
  with an RTSP camera, whether LAN live view is enabled.

When the camera does not work, the camera panel explains why from what the
printer reports: LAN live view disabled on the printer, `ffmpeg` missing on
the server, the RTSP port not reachable, or the login on port 6000 refused
(the X2D, P2S and H2 answer it with a status packet, so bblv reads their
camera over RTSP). The RTSP address is taken from the printer's report
(`ipcam.rtsp_url`), with the configured IP address as the host.

### Filament profile names

The AMS reports a spool's filament profile only by its ID (`tray_info_idx`,
e.g. `GFA00` or `P3f5ea18`). Print files pair these IDs with the profile
names, so bblv learns them from every print file it parses, downloaded or
uploaded, and keeps them in `filaments.json` in the data directory; the
slots of every printer are then named. A slot whose profile has never
appeared in a print file shows no profile name.

## Timelapses

The **Timelapse** section of a printer's page, closed at first, lists the
timelapses on its SD card with their thumbnails, the newest first; the
printer records one when it is turned on when sending a print. Browsers
cannot play the Motion JPEG AVI files the A1 and P1 record, so bblv plays
their frames itself, with pause, seeking and speeds up to 8×, and offers
the original file for download (MP4 timelapses play in the browser).

The printer sends files slowly, the A1 about 0.1 MB/s while printing, so
the first time a recording is opened it is brought into the cache in the
data directory while the player shows how far it has come and the time
left; the window may be closed meanwhile. From then on it opens at once,
until the cache, 1 GB with the geometry, makes room for newer files. bblv
does not delete anything from the SD card.

## Notifications

bblv tells when a print finishes, fails or is paused (e.g. out of filament)
and when a new HMS alert or print error appears, with its description. It
compares consecutive reports, so nothing is told for the state found at
start-up, but a print that finished while the printer was out of reach is.
Both kinds are set up in **Settings**:

- **In this browser**: a system notification while the bblv tab is open in
  the background, or a message on the page while it is visible. Every
  browser agrees to notifications itself, so the setting is per device.
  On phones the browser cannot show system notifications with bblv's
  self-signed certificate (Chrome on Android needs a service worker for
  them, which an untrusted certificate keeps from running; iPhones only
  notify for apps on the home screen); use ntfy there.
- **On your phone with [ntfy](https://ntfy.sh)**: the server sends every
  notification to an ntfy topic, and the ntfy app shows it also while no
  page of bblv is open. Install the app, subscribe to the topic shown in
  the settings (generate a random one: on ntfy.sh, whoever knows a topic
  can read it) and choose the events, the language and the address of bblv
  a notification opens. A self-hosted ntfy server and an access token for a
  protected topic work too; like the access codes, the token is never sent
  back to the browser and has to be entered again when the server changes.
  The notifications, with the names of the printer and the print, leave
  your network for the ntfy server.

## How the 3D view gets the G-code

When a print starts, bblv downloads the print file from the printer's SD
card over FTPS, parses it once and caches the result:

- **A1** (and printers that write `.bbl` files): `cache/<name>.3mf`, which
  contains the sliced G-code and the plate thumbnail; the plate number and
  the AMS mapping come from the `N_<name>.bbl` file the printer writes next
  to it. This is tested with prints sent through the cloud to an A1.
- **X2D, P2S, H2S**: the printer keeps `cache/<name>.gcode.3mf` and reports
  the G-code path inside the printer, `/data/Metadata/plate_N.gcode`; the
  plate number comes from that path.

The SD card is required: printers that print from internal storage without
an SD card (seen on an X2D and an H2D) expose no files over FTPS. bblv then
does not try to download the print, and the printer view says so and offers
the upload. Likewise, if the file
cannot be found, upload the `.gcode` or `.3mf` in the printer view.

Cached geometry is rebuilt automatically when a new version of bblv keeps
more data about a print in the cache.

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

Requirements: Go 1.27.3 (an older `go` command downloads it by itself),
Node.js 22.12+.

```sh
./build.sh          # Linux, macOS, Git Bash
.\build.ps1         # Windows PowerShell
```

Both build the frontend (`web/`, Vue 3 + Element Plus + Three.js) into
`internal/webui/dist/`, which is embedded in the binary, and then build
`dist/bblv-windows-amd64.exe`, `dist/bblv-linux-amd64` and
`dist/bblv-linux-arm64` (pure Go, `CGO_ENABLED=0`).

During development run `bblv serve` and, in `web/`, `npm run dev`; the Vite
dev server proxies `/api` to `https://localhost:8443`.

GitHub Actions runs these checks on every push and pull request, the Go
tests also on Windows, then the build, and keeps the binaries as an
artifact of the run:

```sh
gofmt -l . && go vet ./... && go test -race ./...
go run ./tools/shadowcheck                       # names shadowing others in Go
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...   # known vulnerabilities
cd web && npm run typecheck && npm run shadowcheck   # the same in TypeScript and Vue
npm run i18ncheck                                # translation keys that do not exist
npm test                                         # Vitest
```

The frontend test of the geometry format decodes a file the Go encoder
wrote (`web/src/viewer/testdata/`); after changing the format, rewrite it
with `go test ./internal/gcode -run TestGeometryFixture -update`.

Pushing a tag such as `v0.2.0` runs the same checks and publishes the
binaries with `SHA256SUMS` as a release. Dependabot proposes updates of Go
modules, npm packages and actions once a week, at least 14 days after
their release.

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
