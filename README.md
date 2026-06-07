# soundbuddy

A read-only CLI that inspects the audio stack on a Linux machine and explains,
in plain English, how audio is currently set up — which daemons are running,
how they relate to each other, what hardware ALSA sees, and what the default
input/output devices are.

It **never changes anything**. It only reads state and describes it.

There is no LLM at runtime. The prose is assembled deterministically from facts
the tool discovers (a small rule/template engine), so the binary stays small,
offline, and reproducible.

---

## Goals

- Tell the user, in human language, the shape of their audio stack:
  - "You're running PipeWire 1.6.6, managed by WirePlumber, with the
    PulseAudio and JACK compatibility shims active. ALSA sees two cards…"
- Surface the things people actually want to know:
  - What is the **default output (sink)** and **default input (source)**?
  - Which **sound cards** does the kernel/ALSA see?
  - Which **session/sound server** is actually in charge?
  - Are there **legacy or conflicting** setups (e.g. real PulseAudio *and*
    PipeWire, or JACK running standalone)?
- Degrade gracefully: on a plain ALSA-only box, or a classic PulseAudio box,
  it should still produce a sensible description.

## Non-goals

- Changing volume, switching devices, loading modules — anything mutating.
- Being a full diagnostic/troubleshooting suite. (Could come later.)
- Real-time monitoring. It's a one-shot snapshot.

---

## How it stays statically linked

This is the key design constraint. To compile a **fully static** binary we must
avoid cgo, which means **not** linking against `libasound`, `libpulse`,
`libpipewire`, etc. (those drag in glibc/C deps and break static linking).

Strategy: **zero C linkage.**

1. **Read the kernel directly** for ALSA. Everything under `/proc/asound`
   (`cards`, `version`, `pcm`, `devices`, per-card info) is plain text we parse
   ourselves — no library needed.
2. **Shell out to the tools that already exist** for the userspace daemons and
   parse their output:
   - `pw-dump` → emits **JSON** (ideal; we already confirmed it's available).
   - `wpctl status` → WirePlumber's view (sinks/sources/defaults).
   - `pactl info` / `pactl list` → PulseAudio *or* the pipewire-pulse shim.
   - `aplay -l`, `amixer` → ALSA fallback details.
3. **Scan `/proc`** for running daemons (`pipewire`, `wireplumber`,
   `pulseaudio`, `pipewire-pulse`, `jackd`) to know who's actually alive.

Build target:

```
CGO_ENABLED=0 go build -ldflags '-s -w' -o soundbuddy ./cmd/soundbuddy
```

With `CGO_ENABLED=0` and a pure-Go codebase this produces a static binary on
Linux. **If at any point a feature forces cgo (e.g. we decide we must link
libpipewire directly), the static guarantee breaks — I will stop and flag it
rather than silently shipping a dynamic binary.**

Verification: `file soundbuddy` should report "statically linked", and
`ldd soundbuddy` should report "not a dynamic executable".

A caveat worth stating up front: because we shell out, the external tools
(`pw-dump`, `pactl`, `wpctl`) must be present on the target box for full detail.
The *binary* is static and self-contained; its richest output depends on those
helpers existing. When a helper is missing, the tool says so and falls back to
whatever it can read directly (`/proc/asound`, process list).

---

## Architecture

```
cmd/soundbuddy/main.go      entry point, flag parsing, orchestration
internal/collect/           "collectors" — each gathers raw facts
    alsa.go                 /proc/asound + aplay/amixer
    pipewire.go             pw-dump (JSON), pw-cli
    wireplumber.go          wpctl status
    pulse.go                pactl info/list
    procs.go                running-daemon detection
internal/model/             plain structs describing discovered state (Facts)
internal/explain/           turns Facts -> English (the template/rule engine)
internal/run/               safe command runner (timeouts, missing-binary handling)
```

Flow: **collectors** populate a single `Facts` struct → **explain** walks that
struct and emits prose → printed to stdout.

### The "explain" engine (no LLM)

A set of small rules, each pattern-matching on the collected `Facts` and
emitting a sentence/paragraph. Roughly:

- Stack identification rule: PipeWire present + WirePlumber present →
  "modern PipeWire stack." PulseAudio daemon present and no PipeWire →
  "classic PulseAudio stack." Neither → "bare ALSA."
- Shim rule: pipewire-pulse running → "the PulseAudio API is being emulated
  by PipeWire, so PulseAudio apps still work."
- Conflict rule: real `pulseaudio` *and* `pipewire` both alive → warn.
- Default-device rule: name the default sink/source in friendly terms.
- Hardware rule: list ALSA cards and what they are.

Output is ordered from the big picture (what server runs the show) down to
specifics (cards, defaults).

---

## Output design

A full structured readout, rendered as aligned tables (via `text/tabwriter`),
in fixed sections:

```
SOUNDBUDDY — AUDIO SYSTEM READOUT
host: goodlife2

══ STACK ══              daemons: PipeWire / WirePlumber / pipewire-pulse / PulseAudio / JACK, with PID + status
══ ALSA CARDS ══         every card: id, driver, PCI slot, codec(s), all PCM devices, ACTIVE/IDLE
══ PIPEWIRE DEVICES ══   every Audio/Device, mapped to its ALSA card, ACTIVE/idle, endpoint counts
══ SINKS (outputs) ══    every sink: id, name, state, volume, card, device-path; * marks default
══ SOURCES (inputs) ══   every source, same columns
══ DEFAULTS ══           default output / input
══ SIGNAL FLOW ══        per card, how the layers link: hardware → ALSA (PCMs) → PipeWire device → nodes,
                         showing each subsystem's label for the same thing, the device-path decode, and
                         (for HDMI) the connected display
══ NOTES ══              conflicts or missing-helper caveats (only when present)
══ GLOSSARY ══           every acronym/term used, grouped (layers, hardware, PipeWire, notation, states)
```

The readout shows **all** cards and devices whether in use or not, and marks
which are ACTIVE. Acronyms are defined at the bottom.

Flags:

- `--no-color`        : disable ANSI styling.
- `--glossary` / `-g` : include the glossary section (off by default).
- `--json`           : (deferred) emit the structured `Facts` for scripts.

---

## Detection matrix (what we look for)

| Layer        | Primary source            | Fallback              |
|--------------|---------------------------|-----------------------|
| Kernel/ALSA  | `/proc/asound/*`          | `aplay -l`            |
| PipeWire     | `pw-dump` (JSON)          | `/proc` process scan  |
| WirePlumber  | `wpctl status`            | `/proc` process scan  |
| PulseAudio   | `pactl info` / `list`     | `/proc` process scan  |
| JACK         | `/proc` process scan      | —                     |
| Defaults     | `pactl info`, `wpctl`     | pw-dump metadata      |

---

## Locked scope (decided)

- **Portability:** Target *this machine's stack* — PipeWire + WirePlumber with
  the pipewire-pulse shim. Code stays lean and tuned for that topology. Other
  stacks (bare ALSA, classic PulseAudio) get a basic best-effort description but
  are not a focus.
- **Output:** **Full structured readout** (not prose). Aligned tables listing
  every ALSA card (with driver, PCI slot, codec and PCM devices), every PipeWire
  device, every sink/source, the defaults, and a thorough **glossary** that
  defines all the acronyms used. ACTIVE vs IDLE is marked for each card/device.
- **JSON:** **Not in v1.** `--json` can be added later.
- **Missing helpers:** **Note and continue.** If `pw-dump` / `pactl` / `wpctl`
  is absent, note it in the NOTES section, fall back to what `/proc` and the
  process scan provide, and keep going.

### v1 flags

- `--no-color`        : disable ANSI styling.
- `--glossary` / `-g` : include the glossary section (off by default).
- ~~`--json`~~       : deferred.
