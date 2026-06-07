# soundbuddy

A read-only CLI that inspects the audio stack on a Linux machine and prints a
full, structured **readout** of how audio is set up — which daemons are running,
what hardware ALSA sees, which PipeWire devices and sinks/sources exist, what the
defaults are, and how all the layers link together.

It **never changes anything**. It only reads state and reports it.

There is no LLM at runtime. Every line is assembled deterministically from facts
the tool discovers, so the binary stays small, offline, and reproducible.

---

## Goals

- Give a complete picture of the audio stack at a glance:
  - which sound server is in charge (PipeWire / WirePlumber / pulse shim), with
    PIDs and versions;
  - every ALSA card the kernel sees, with driver, PCI slot, codec(s) and PCM
    devices — whether in use or not;
  - every PipeWire device and sink/source, marked ACTIVE or IDLE;
  - the default output and input (resolving the `.monitor` fallback so it
    matches `pactl info`).
- Make the cross-layer wiring legible: a SIGNAL FLOW view showing how the *same*
  device is labelled differently by the kernel vs PipeWire, and how those labels
  link up (including the HDMI connector-vs-PCM numbering gotcha).
- Define every acronym/term it uses, on request (`-g`).

## Non-goals

- Changing volume, switching devices, loading modules — anything mutating.
- A full troubleshooting suite. It's a one-shot snapshot.
- Deep portability. It targets a modern PipeWire stack (see *Scope* below);
  other stacks get a best-effort readout but aren't the focus.

---

## Build & run

```
CGO_ENABLED=0 go build -ldflags '-s -w' -o soundbuddy ./cmd/soundbuddy
./soundbuddy            # full readout
./soundbuddy -g         # also print the glossary
./soundbuddy --no-color # plain text (also auto-disabled when piped)
```

### Flags

- `--no-color`        : disable ANSI styling (also off automatically when stdout
  isn't a terminal, or when `NO_COLOR` is set).
- `--glossary` / `-g` : append the glossary section (off by default).
- `--json`            : *deferred* — would emit the structured `Facts` for scripts.

---

## How it stays statically linked

The key constraint. A **fully static** binary means avoiding cgo, which means
**not** linking `libasound` / `libpulse` / `libpipewire` (they pull in glibc/C
deps and break static linking).

Strategy: **zero C linkage.**

1. **Read the kernel directly** for ALSA — all plain-text/sysfs, no library:
   - `/proc/asound/cards`, `/proc/asound/version`
   - `/proc/asound/pcm` (the flat PCM-device list)
   - `/proc/asound/card*/codec#*` (codec chips)
   - `/sys/class/sound/card*/device` → PCI slot and kernel driver (symlinks)
2. **Scan `/proc/<pid>/comm`** for the running daemons (`pipewire`,
   `pipewire-pulse`, `wireplumber`, `pulseaudio`, `jackd`/`jackdbus`).
3. **Shell out to two helpers** and parse their output:
   - `pw-dump` → **JSON**: nodes (sinks/sources), devices, and the `default`
     metadata (default sink/source).
   - `wpctl status` → the PipeWire version (from its header) and per-node volumes.

That's it — no `pactl`, `aplay`, or `amixer` are invoked. `pw-jack` is probed via
`PATH` (not run) to report JACK-bridge availability.

With `CGO_ENABLED=0` and a pure-Go codebase this produces a static binary on
Linux. **If a future feature ever forces cgo (e.g. linking libpipewire), the
static guarantee breaks — that should be surfaced, not shipped silently.**

Verify:

```
file soundbuddy   # → "... statically linked ..."
ldd soundbuddy    # → "not a dynamic executable"
```

Because the rich detail comes from shelling out, `pw-dump` / `wpctl` should be
present for full output. The *binary* is self-contained regardless; when a helper
is missing the tool notes it (NOTES section) and falls back to what `/proc` and
`/sys` provide.

---

## Architecture

```
cmd/soundbuddy/main.go        entry point: flags, colour decision, orchestration
internal/run/run.go           safe command runner (5s timeout, missing-binary vs error)
internal/model/facts.go       inert structs describing discovered state (Facts)
internal/collect/             collectors — each gathers raw facts
    collect.go                All(): runs collectors, reconciles versions/volumes/defaults
    alsa.go                   /proc/asound + /sys  (cards, PCMs, codecs, driver, PCI)
    pipewire.go               pw-dump (JSON): nodes, devices, default metadata
    wireplumber.go            wpctl status: version + volumes
    procs.go                  running-daemon detection via /proc
internal/explain/explain.go   turns Facts -> the readout (tabwriter tables + flow + glossary)
```

Flow: **collectors** populate one `Facts` struct → `collect.All()` reconciles the
bits that come from more than one source → **explain** renders the readout to
stdout. The explanation logic lives entirely in `explain`; collectors never
format prose.

### Reconciliation (`collect.All`)

A few facts are stitched together from multiple sources:

- **PipeWire version** — `pw-dump` rarely exposes `core.version`, so it falls
  back to the version in the `wpctl status` header.
- **Volumes** — parsed from `wpctl status` and matched onto PipeWire nodes by
  global id.
- **Defaults** — the default sink/source come from `pw-dump`'s `default`
  metadata object. When no default *source* is configured, the effective input
  is the default sink's `.monitor` (the PulseAudio-shim behaviour), so that
  fallback is computed here to match `pactl info`.

---

## Output sections

```
SOUNDBUDDY — AUDIO SYSTEM READOUT
host: <hostname>

══ STACK ══              daemons (PipeWire / WirePlumber / pipewire-pulse / PulseAudio / JACK):
                         version, running status, PID
══ ALSA CARDS ══         every card: id, driver, PCI slot, codec(s), all PCM devices; ACTIVE/IDLE
══ PIPEWIRE DEVICES ══   every Audio/Device, mapped to its ALSA card; ACTIVE/idle + endpoint counts
══ SINKS (outputs) ══    every sink: id, name, state, volume, card, device-path; * = default;
                         MONITOR-SRC column flags the sink whose .monitor is the default input
══ SOURCES (inputs) ══   real hardware sources (monitors are not faked in here)
══ DEFAULTS ══           default output / input (input resolves the .monitor fallback)
══ SIGNAL FLOW ══        wired-up paths only: per card, hardware → PipeWire device → node(s),
                         with each subsystem's label, the device-path decode (HDMI connector vs
                         PCM number), the connected display, and the node's .monitor source
══ NOTES ══              conflicts (e.g. real PulseAudio + PipeWire) or missing-helper caveats
══ GLOSSARY ══           (only with -g) every term used, grouped: layers, hardware, PipeWire,
                         device-name notation, node states, status labels
```

ALSA CARDS and PIPEWIRE DEVICES list **everything** whether in use or not, marked
ACTIVE/IDLE. SIGNAL FLOW deliberately shows **only** components wired into the
graph (a card with at least one sink/source, even if idle/suspended) — it's a map
of live connections, not a repeat of the full card list.

### Sample (this machine)

```
══ STACK ══
  COMPONENT       VERSION  STATUS       DETAIL
  PipeWire        1.6.6    running      pid 853
  WirePlumber     1.6.6    running      pid 854, session manager
  pipewire-pulse  shim     running      pid 4538, PulseAudio API
  PulseAudio      -        not running  standalone daemon
  JACK (jackd)    -        not running  pw-jack bridge not installed

══ ALSA CARDS (kernel) — 2 ══
  card 0  [PCH]  HDA Intel PCH   <IDLE>
      driver: snd_hda_intel   pci: 0000:00:1f.3
      codec: Realtek ALC892
      PCM devices (card,device):
        0,0  ALC892 Analog      playback + capture
        0,2  ALC892 Alt Analog  capture
  card 1  [HDMI]  HDA ATI HDMI   <ACTIVE>
      driver: snd_hda_intel   pci: 0000:01:00.1
      codec: ATI R6xx HDMI
      PCM devices (card,device):
        1,3 HDMI 0 … 1,11 HDMI 5  (six HDMI playback PCMs)

══ SINKS (outputs) — 1 ══
     ID     NAME                              STATE      VOL  ALSA    DEVICE-PATH  MONITOR-SRC
  *  id 50  … Digital Stereo (HDMI 4)         suspended  40%  card 1  hdmi:1,3     ★ default input

══ DEFAULTS ══
  output:  id 50  … Digital Stereo (HDMI 4)
  input:   …hdmi-stereo-extra3.monitor  [monitor of output id 50]  (falls back to this output's monitor)

══ SIGNAL FLOW (wired-up paths only) ══
  card 1  «HDA ATI HDMI»  id=HDMI
    hardware │ 0000:01:00.1 · driver snd_hda_intel · codec ATI R6xx HDMI
    PipeWire │ device 42  «Ellesmere HDMI Audio [Radeon RX 470/480 / 570/580/590]»
             │   └─ sink id 50  ★default
             │        label   «… Digital Stereo (HDMI 4)» · 40% · suspended
             │        opens   hdmi:1,3  →  card 1, HDMI connector index 3 (shown one-based as "HDMI 4")
             │        ALSA reports endpoint: «PHL 279P1»
             │        monitor «…hdmi-stereo-extra3.monitor»  ★default input (loopback capture of this output)
```

---

## Data sources (what feeds each section)

| Section          | Source(s)                                                        |
|------------------|-----------------------------------------------------------------|
| STACK            | `/proc/<pid>/comm`; versions from `pw-dump` / `wpctl`            |
| ALSA CARDS       | `/proc/asound/{cards,version,pcm,card*/codec#*}`, `/sys/class/sound` |
| PIPEWIRE DEVICES | `pw-dump` (`Audio/Device` objects, `alsa.card`)                  |
| SINKS / SOURCES  | `pw-dump` (`Audio/Sink`/`Audio/Source` nodes); volumes from `wpctl status` |
| DEFAULTS         | `pw-dump` `default` metadata; `.monitor` fallback computed       |
| SIGNAL FLOW      | the above, joined by ALSA card index + the node's `alsa.path`/`alsa.name` |

---

## Scope (decided)

- **Portability:** tuned for *this machine's stack* — PipeWire + WirePlumber with
  the pipewire-pulse shim. Other stacks (bare ALSA, classic PulseAudio) get a
  best-effort readout; the STACK section still identifies them.
- **Output:** a full structured readout (not prose), via `text/tabwriter`.
- **JSON:** not in v1; `--json` can be added later.
- **Missing helpers:** note in NOTES and continue, falling back to `/proc`/`/sys`.
