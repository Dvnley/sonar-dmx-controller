# Sonar DMX Controller

A small Mac app that drives the lighting rig over Art-Net. It runs a web UI at
<http://127.0.0.1:8080> and sends ArtDmx to a DMX-AN2 node (default `2.0.0.1`,
universe 0) at 40 frames per second, finding the node with ArtPoll.

v0.3 is a rebuild of v0.2 (whose source was lost). It keeps the v0.2 API and
adds fixture profiles, an editable patch and a channel sweep page.

## Signal chain

MacBook → USB-C Ethernet → DMX-AN2 (Art-Net node) → Port A DMX OUT → wireless
DMX transmitter → receivers on the fixtures.

- Mac USB Ethernet: IPv4 **manual**, `2.0.0.2`, subnet `255.0.0.0`, no router.
- DMX-AN2: `2.0.0.1`, Port A set to Output, Art-Net, universe 0.
- Only run one Art-Net sender at a time (this app, *or* a probe script, *or*
  another lighting app). Two senders make the lights flicker.

## Running it

1. Download `Sonar_DMX_Controller_v0.3` (from the CI build artifact, or build it
   yourself, see below).
2. Double-click `START-APPLE-SILICON.command` (M1 and newer) or
   `START-INTEL.command`. The browser opens on the controller.
3. If macOS says it "could not verify" the script, click **Done** (not Move
   to Trash). Then either open System Settings, Privacy & Security, and
   click **Open Anyway** at the bottom (once for the script, once for the
   binary), or unblock the whole folder in Terminal:
   `xattr -dr com.apple.quarantine <drag the folder here>`.
   This happens because the app is not signed with an Apple developer ID.

The status pill at the top says whether the Mac has a `2.x.x.x` address and
whether the AN2 answered ArtPoll. The Setup tab has the details.

Settings, the patch and the last values are saved to
`~/Library/Application Support/SonarDMX/config.json`. Delete that file to go
back to the default rig.

Command-line flags: `-addr 127.0.0.1:8080`, `-config <path>`, `-no-browser`,
`-remote`, `-pin <digits>`.

## iPad remote

The Mac still sends the Art-Net; the iPad is a touch remote for it.

1. Start the app with `START-WITH-IPAD.command` instead of the other START
   script. The Mac and the iPad must be on the same Wi-Fi.
2. On the Mac, the Setup tab (and the Terminal window) shows the address to
   open on the iPad, such as `http://Danleys-MacBook.local:8080`, and a
   6-digit PIN.
3. On the iPad, open that address in Safari and enter the PIN. Then tap
   Share, **Add to Home Screen** to get an app icon.

The PIN is made once and saved in the config, so the iPad stays signed in.
Other devices without it see only the PIN page. The Mac itself never needs
the PIN. If the iPad can't connect, check that macOS allowed incoming
connections for the app (System Settings, Network, Firewall).

## The rig (default patch)

| Fixture | Profile | Mode | Universe | Address |
| --- | --- | --- | --- | --- |
| Uplight 1-6 | Chauvet Freedom Flex H4 IP | 6CH (R G B A W UV) | 0 | 1, 7, 13, 19, 25, 31 |
| Wash FX | Chauvet Wash FX | 3CH (R G B) | 0 | 17 |
| Spot | Generic moving-head spot | 9CH | 0 | 1 |

**These addresses overlap.** The uplights cover 1-36, which collides with the
spot (1-9) and the wash (17-19). The app shows a warning until one group is
moved, either to free addresses (for example uplights at 41 and up) or to
universe 1. Where fixtures overlap, the highest value wins on each channel.

The Wash FX mode was inferred from a test (sending 255 on channel 19 turned it
blue). The spot's channel layout is a guess for a common cheap 9CH head:

1 Pan · 2 Tilt · 3 Color wheel · 4 Gobo · 5 Shutter/strobe · 6 Dimmer ·
7 Pan/tilt speed · 8 Auto/sound · 9 Reset

There is a second mode with dimmer and shutter swapped. If neither matches,
open the spot's channel map in the Patch tab and rename or retype the
channels.

## Pages

- **Perform** (the default, built for the iPad). Pick a layout at the top:
  - **Groups**: a column per fixture type (uplights, wash, spot) with color
    pads, a level fader, a FLASH button for just that group and a Programs
    button. The spot column has a pan/tilt pad and a dimmer instead.
  - **Desk**: vertical faders like a lighting console. One level fader and
    Flash per group, R/G/B/A/W/UV faders for the group picked with **Sel**,
    program speed, and master with blackout.
  - **Stage**: a map of the room. Tap lights (or All / a group) to select
    them, then tap a color, set the level or start a program for just those.
    Tick **Move lights** and drag them to where they really are; positions
    are saved with the patch. Running programs show live on the map.
  - **Pads**: the original big color pads, color wheel, strobe and rainbow.

  Under every layout: **FLASH** (hold for full white), **BLACKOUT** and
  master, then **Programs**: Chase, Color jump, Color fade, Rainbow, Wave,
  Pulse, Strobe pop, Police, Fire and Sparkle. Tap one to start it on the
  lights shown next to the title. **Speed** runs from 30 to 240 BPM,
  **Size** goes from all lights together to spread across them, and
  **Variation** picks the colors (Rainbow, Warm, Cool, Party, Red/Blue,
  Pink/Purple). Tapping a color on a light takes it out of its program.

  **Music mode** makes programs step on the beat; Speed then picks ¼ to 4
  steps per beat. Tap **TAP** along with the music (4 taps sets the tempo),
  or press **Listen with this Mac's mic** in the browser on the Mac to
  follow the kick drum. The mic only works on the Mac (browsers block it on
  the iPad over Wi-Fi), so on the iPad use TAP.

  The fade chips set how long color changes and scene recalls take. Scenes:
  tap **Save to…** then a slot to store the current look, tap a saved scene
  to fade to it (this stops programs). On the RGB-only wash, the Amber,
  White, UV and Warm pads are mixed from red, green and blue.
- **Fixtures**: master, blackout, RGBW test, color presets and mixer for all (or
  ticked) color fixtures, and a card per fixture with brightness and a fader
  per channel. Moving heads get a pan/tilt pad plus Center, Light on/off and
  Reset buttons.
- **Patch**: name, profile, mode, universe and address per fixture, an
  editable channel map (name, type, default value), a **Free** button that
  moves a fixture to the first free address, overlap warnings and a DMX map.
  Nothing changes until you press **Save patch**.
- **Channel sweep**: the old `artnet.py` probe, built in. It takes over the
  output while it runs (everything not listed is 0), so you never need to stop
  the app to probe. Use **Manual** to step with Next/Prev and type what each
  channel did; notes are kept in the browser so you can copy them out.
  Presets: spot pass 1 (channels 1-9 alone), spot pass 2 (pan/tilt centered,
  dimmer and shutter open) and the wash R, G, B check. You can also paste a
  probe script such as `5:17=255;5:18=255;5:19=255;5:17=255,18=255,19=255;2:`.
- **Setup**: node IP, frame rate, diagnostics, and the nodes that answered
  ArtPoll.

## API

All POST bodies are JSON. Every POST returns the full state (like
`GET /api/state`) or `{"error": "..."}` with status 400.

v0.2 endpoints:

| Endpoint | Body | Notes |
| --- | --- | --- |
| `GET /api/diag` | | `{an2_online, artnet_ip_ok, local_ips, target, ...}` |
| `GET /api/state` | | Everything, including the patch and overlaps |
| `POST /api/test` | `{}` | Red, green, blue, white, one second each |
| `POST /api/blackout` | `{}` or `{"on": true}` | `{}` toggles |
| `POST /api/color` | `{"fixtures": "all" \| [i], "r", "g", "b", "a", "w", "uv"}` | 0-255, missing colors are left alone; `"dimmer"` also accepted |
| `POST /api/fixture` | `{"index", "brightness"}` or `{"index", "enabled"}` | brightness 0-100 |
| `POST /api/master` | `{"value"}` | 0-100 |
| `POST /api/config` | `{"target_ip", "fps", "universe", "addresses", "names"}` | `universe`, `addresses`, `names` apply to the uplights in order |

Fixture indexes are 0-based positions in the patch.

New in v0.3:

| Endpoint | Body |
| --- | --- |
| `POST /api/fixture` | `{"index", "values": {"pan": 128, "6": 255, "Gobo": 40}}`: a channel by type, 1-based number or name |
| `POST /api/fixture/reset` | `{"index"}`: back to channel defaults |
| `GET /api/profiles` | Built-in fixture profiles |
| `POST /api/patch` | `{"fixtures": [...]}`: replace the patch |
| `POST /api/sweep` | `{"universe", "plan": {"from", "to", "value", "hold", "gap", "base", "manual"}}` or `{"universe", "script", "manual"}` |
| `POST /api/sweep/step` | `{"delta": 1}` or `-1` for a manual sweep |
| `POST /api/sweep/stop` | `{}` |
| `POST /api/poll` | `{}`: send ArtPoll now |
| `POST /api/color` | also takes `"fade"` in seconds |
| `POST /api/flash` | `{"on": true}` while held, `{"on": false}` on release |
| `POST /api/strobe` | `{"hz": 5}`, 0 stops |
| `POST /api/rainbow` | `{"seconds": 10}` per cycle, 0 stops |
| `POST /api/scene/save`, `/recall`, `/clear` | `{"slot": 0-7, "name", "fade"}` |
| `POST /api/flash` | also takes `"fixtures": [i]` to flash only those |
| `GET /api/programs` | The program and variation catalog |
| `POST /api/program` | `{"fixtures": "all" \| [i], "program": "chase", "speed": 0-100, "size": 0-100, "variation": 0-5, "music": false}` |
| `POST /api/program/stop` | `{"fixtures": [i]}`, or `{}` to stop everything |
| `POST /api/beat` | `{"source": "tap" \| "mic"}`: one beat, returns 204 |
| `POST /api/group` | `{"fixtures": [i], "brightness": 0-100, "enabled": true}`, either field optional |
| `POST /api/position` | `{"index", "x", "y"}`: stage map position, 0-1 |

Example: `curl -s -X POST localhost:8080/api/color -d '{"fixtures":"all","r":255}'`

## Building

Needs Go 1.23 or newer.

```sh
make test                 # vet + unit tests
go run . -no-browser      # run from source
./scripts/build-mac.sh    # both Mac binaries + START scripts in dist/
```

`build-mac.sh` cross-compiles from any OS, so it also runs in CI. Every push
uploads a ready-to-run `Sonar_DMX_Controller_v0.3` folder as a build artifact.

## Code layout

- `main.go`: flags, starts the web server and the output loop
- `internal/artnet`: ArtDmx and ArtPoll packets, the UDP socket
- `internal/fixture`: profiles, patch, overlap check, frame rendering
- `internal/controller`: show state, RGBW test, channel sweep, config file
- `internal/server`: HTTP API and the embedded web UI (`web/`)
