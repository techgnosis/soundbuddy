// Package model defines the plain data structures that describe the discovered
// audio state. Collectors fill these in; the explain package turns them into a
// readout. Keeping the data inert (no behaviour) means the rendering logic lives
// in exactly one place.
package model

// Facts is the complete snapshot of the audio stack the tool gathered.
type Facts struct {
	Host        string
	Procs       Procs
	ALSA        ALSA
	PipeWire    PipeWire
	WirePlumber WirePlumber

	// MissingTools lists helper binaries we wanted but could not find, so the
	// report can note degraded detail instead of pretending.
	MissingTools []string
}

// Process is a single running daemon we care about.
type Process struct {
	Name string
	PID  int
}

// Procs records which audio daemons are alive, discovered from /proc.
type Procs struct {
	PipeWire      *Process
	PipeWirePulse *Process // the PulseAudio-compat shim
	WirePlumber   *Process
	PulseAudio    *Process // a *real* standalone PulseAudio daemon
	JACK          *Process // jackd / jackdbus
}

// ALSA is what the kernel sound layer exposes via /proc/asound.
type ALSA struct {
	Version string // driver version string, e.g. "k7.0.11-1-cachyos"
	Cards   []Card
	Read    bool // true if /proc/asound was readable at all
}

// Card is one ALSA sound card.
type Card struct {
	Index    int
	ID       string // short id, e.g. "PCH"
	Name     string // human name, e.g. "HDA Intel PCH"
	LongName string // fuller description line
	Driver   string // kernel module, e.g. "snd_hda_intel"
	PCI      string // PCI slot, e.g. "0000:00:1f.3" (empty if not a PCI device)
	Codecs   []string // codec chips, e.g. "Realtek ALC892"
	PCMs     []PCM    // the PCM (playback/capture) devices on this card
}

// PCM is one playback/capture device on a card (the "device" half of card,device
// addressing such as hw:0,3).
type PCM struct {
	Device   int    // device number within the card
	Name     string // e.g. "ALC892 Analog", "HDMI 0"
	Playback bool   // can play audio out
	Capture  bool   // can record audio in
}

// PipeWire holds what we learned from pw-dump / wpctl about the PipeWire graph.
type PipeWire struct {
	Running bool
	Version string // e.g. "1.6.6"

	DefaultSinkName   string // node.name of the default sink, from metadata
	DefaultSourceName string // node.name of the default source

	Sinks   []Node
	Sources []Node
	Devices []Device

	// HasJACK is true if a JACK bridge (pw-jack) is available on the box.
	HasJACK bool
}

// Node is an audio endpoint (a sink or source) in the PipeWire graph.
type Node struct {
	ID          int    // PipeWire global id (matches the number wpctl prints)
	Name        string // node.name, the stable identifier
	Description string // node.description, the friendly label
	MediaClass  string // e.g. "Audio/Sink"
	State       string // PipeWire node state: "running", "suspended", "idle", …
	IsDefault   bool
	VolumePct   int  // percent; valid only when HasVolume is true
	HasVolume   bool

	// Hardware linkage (from the node's ALSA props). AlsaCard is -1 when the
	// node is not backed by an ALSA card (e.g. a virtual/loopback node).
	AlsaCard int    // index matching model.Card.Index, or -1
	AlsaPath string // e.g. "hdmi:1,3" — card,device within ALSA
	AlsaName string // alsa.name prop; for HDMI this is the connected display, e.g. "PHL 279P1"
}

// Device is a PipeWire Audio/Device object — one physical sound device, which
// usually corresponds one-to-one with an ALSA card. A device can exist with no
// active sink/source (e.g. its profile is off), which is exactly how we tell
// "present but idle" hardware from "in use" hardware.
type Device struct {
	ID          int
	Description string // device.description, e.g. "Built-in Audio"
	Nick        string // device.nick, e.g. "HDA Intel PCH"
	AlsaCard    int    // index matching model.Card.Index, or -1
}

// WirePlumber records the session-manager view (mostly volumes and defaults).
type WirePlumber struct {
	Running bool
	Version string
}
