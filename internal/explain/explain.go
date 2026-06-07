// Package explain turns a model.Facts snapshot into a full, structured readout
// of the audio system: every card, every device, what is active, and a glossary
// defining the terms used. No LLM is involved — it is deterministic formatting.
package explain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"soundbuddy/internal/model"
)

// Options controls presentation only, not content.
type Options struct {
	Color    bool
	Glossary bool
}

// Render produces the full readout.
func Render(f model.Facts, opt Options) string {
	s := style{on: opt.Color}
	var b strings.Builder

	writeHeader(&b, s, f)
	writeStack(&b, s, f)
	writeCards(&b, s, f)
	writeDevices(&b, s, f)
	writeNodes(&b, s, f)
	writeDefaults(&b, s, f)
	writeFlow(&b, s, f)
	writeNotes(&b, s, f)
	if opt.Glossary {
		writeGlossary(&b, s, f)
	}

	return b.String()
}

// ─── styling (kept off the inside of aligned tables, where ANSI bytes would
// break tabwriter's column-width maths) ───────────────────────────────────────

type style struct{ on bool }

func (s style) section(t string) string {
	if !s.on {
		return t
	}
	return "\033[1;36m" + t + "\033[0m"
}
func (s style) dim(t string) string {
	if !s.on {
		return t
	}
	return "\033[2m" + t + "\033[0m"
}
func (s style) warn(t string) string {
	if !s.on {
		return t
	}
	return "\033[1;33m" + t + "\033[0m"
}

func section(b *strings.Builder, s style, title string) {
	b.WriteString("\n" + s.section("══ "+title+" ══") + "\n")
}

// tabBlock renders rows as an aligned table, indented by indent.
func tabBlock(indent string, rows [][]string) string {
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()

	var out strings.Builder
	for _, ln := range strings.Split(strings.TrimRight(sb.String(), "\n"), "\n") {
		out.WriteString(indent + ln + "\n")
	}
	return out.String()
}

// ─── sections ─────────────────────────────────────────────────────────────────

func writeHeader(b *strings.Builder, s style, f model.Facts) {
	host := f.Host
	if host == "" {
		host = "(unknown)"
	}
	b.WriteString(s.section("SOUNDBUDDY — AUDIO SYSTEM READOUT") + "\n")
	b.WriteString(s.dim("host: "+host) + "\n")
}

func writeStack(b *strings.Builder, s style, f model.Facts) {
	section(b, s, "STACK")
	p := f.Procs

	rows := [][]string{{"COMPONENT", "VERSION", "STATUS", "DETAIL"}}
	rows = append(rows, daemonRow("PipeWire", f.PipeWire.Version, p.PipeWire, ""))
	rows = append(rows, daemonRow("WirePlumber", f.WirePlumber.Version, p.WirePlumber, "session manager"))
	rows = append(rows, daemonRow("pipewire-pulse", "shim", p.PipeWirePulse, "PulseAudio API"))
	rows = append(rows, daemonRow("PulseAudio", "", p.PulseAudio, "standalone daemon"))

	jackDetail := "pw-jack bridge not installed"
	if f.PipeWire.HasJACK {
		jackDetail = "pw-jack bridge installed"
	}
	rows = append(rows, daemonRow("JACK (jackd)", "", p.JACK, jackDetail))

	b.WriteString(tabBlock("  ", rows))
}

func daemonRow(name, version string, proc *model.Process, detail string) []string {
	status := "not running"
	pid := "-"
	if proc != nil {
		status = "running"
		pid = "pid " + strconv.Itoa(proc.PID)
	}
	if version == "" {
		version = "-"
	}
	if detail != "" && pid != "-" {
		detail = pid + ", " + detail
	} else if detail == "" {
		detail = pid
	}
	return []string{name, version, status, detail}
}

func writeCards(b *strings.Builder, s style, f model.Facts) {
	section(b, s, fmt.Sprintf("ALSA CARDS (kernel) — %d", len(f.ALSA.Cards)))
	if !f.ALSA.Read {
		b.WriteString("  (could not read /proc/asound)\n")
		return
	}
	if len(f.ALSA.Cards) == 0 {
		b.WriteString("  (no cards detected)\n")
		return
	}

	for _, c := range f.ALSA.Cards {
		status := "IDLE"
		if cardActive(f, c.Index) {
			status = "ACTIVE"
		}
		fmt.Fprintf(b, "  card %d  [%s]  %s   <%s>\n", c.Index, c.ID, cardName(c), status)

		fmt.Fprintf(b, "      driver: %s   pci: %s\n", orDash(c.Driver), orDash(c.PCI))
		if len(c.Codecs) > 0 {
			fmt.Fprintf(b, "      codec: %s\n", strings.Join(c.Codecs, ", "))
		}

		if len(c.PCMs) > 0 {
			b.WriteString("      PCM devices (card,device):\n")
			pcmRows := [][]string{}
			for _, p := range c.PCMs {
				pcmRows = append(pcmRows, []string{
					fmt.Sprintf("%d,%d", c.Index, p.Device),
					p.Name,
					pcmDir(p),
				})
			}
			b.WriteString(tabBlock("        ", pcmRows))
		}
	}
}

func writeDevices(b *strings.Builder, s style, f model.Facts) {
	section(b, s, fmt.Sprintf("PIPEWIRE DEVICES — %d", len(f.PipeWire.Devices)))
	if len(f.PipeWire.Devices) == 0 {
		b.WriteString("  (none)\n")
		return
	}

	devs := append([]model.Device(nil), f.PipeWire.Devices...)
	sort.Slice(devs, func(i, j int) bool { return devs[i].ID < devs[j].ID })

	rows := [][]string{{"ID", "DESCRIPTION", "ALSA", "STATUS", "ENDPOINTS"}}
	for _, d := range devs {
		nsink := len(nodesForCard(f.PipeWire.Sinks, d.AlsaCard))
		nsrc := len(nodesForCard(f.PipeWire.Sources, d.AlsaCard))
		status := "idle"
		if nsink+nsrc > 0 {
			status = "ACTIVE"
		}
		alsa := "-"
		if d.AlsaCard >= 0 {
			alsa = "card " + strconv.Itoa(d.AlsaCard)
		}
		rows = append(rows, []string{
			"dev " + strconv.Itoa(d.ID),
			deviceLabel(d),
			alsa,
			status,
			fmt.Sprintf("%d sink, %d src", nsink, nsrc),
		})
	}
	b.WriteString(tabBlock("  ", rows))
}

func writeNodes(b *strings.Builder, s style, f model.Facts) {
	writeNodeSection(b, s, "SINKS (outputs)", f.PipeWire.Sinks, true, f)
	writeNodeSection(b, s, "SOURCES (inputs)", f.PipeWire.Sources, false, f)
}

func writeNodeSection(b *strings.Builder, s style, title string, nodes []model.Node, isSink bool, f model.Facts) {
	section(b, s, fmt.Sprintf("%s — %d", title, len(nodes)))
	if len(nodes) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	sorted := append([]model.Node(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	header := []string{"", "ID", "NAME", "STATE", "VOL", "ALSA", "DEVICE-PATH"}
	if isSink {
		header = append(header, "MONITOR-SRC")
	}
	rows := [][]string{header}
	for _, n := range sorted {
		marker := " "
		if n.IsDefault {
			marker = "*"
		}
		alsa := "-"
		if n.AlsaCard >= 0 {
			alsa = "card " + strconv.Itoa(n.AlsaCard)
		}
		row := []string{
			marker,
			"id " + strconv.Itoa(n.ID),
			nodeLabel(n),
			orDash(n.State),
			volStr(n),
			alsa,
			orDash(n.AlsaPath),
		}
		if isSink {
			mon := "available"
			if monitorIsDefault(f, n) {
				mon = "★ default input"
			}
			row = append(row, mon)
		}
		rows = append(rows, row)
	}
	b.WriteString(tabBlock("  ", rows))
	b.WriteString(s.dim("  (* = current default)") + "\n")
	if isSink {
		b.WriteString(s.dim("  Every sink has a .monitor source — a loopback input that captures whatever it plays.") + "\n")
	}
}

func writeDefaults(b *strings.Builder, s style, f model.Facts) {
	section(b, s, "DEFAULTS")
	sink := findDefault(f.PipeWire.Sinks, f.PipeWire.DefaultSinkName)

	rows := [][]string{
		{"output:", defaultStr(sink)},
		{"input:", defaultSourceStr(f)},
	}
	b.WriteString(tabBlock("  ", rows))
}

// defaultSourceStr resolves the default input, which may be a real source or —
// when none is configured — a sink's .monitor (matching what pactl reports).
func defaultSourceStr(f model.Facts) string {
	name := f.PipeWire.DefaultSourceName
	if name == "" {
		return "(none configured)"
	}
	if n := findDefault(f.PipeWire.Sources, name); n != nil {
		return defaultStr(n)
	}
	if sink := sinkByMonitorName(f, name); sink != nil {
		note := ""
		if f.PipeWire.DefaultSourceFallback {
			note = "  (no input device set — falls back to this output's monitor)"
		}
		return fmt.Sprintf("%s  [monitor of output id %d]%s", name, sink.ID, note)
	}
	return name
}

// monitorIsDefault reports whether the given sink's monitor is the default input.
func monitorIsDefault(f model.Facts, sink model.Node) bool {
	return f.PipeWire.DefaultSourceName == sink.Name+".monitor"
}

func sinkByMonitorName(f model.Facts, monitor string) *model.Node {
	for i := range f.PipeWire.Sinks {
		if f.PipeWire.Sinks[i].Name+".monitor" == monitor {
			return &f.PipeWire.Sinks[i]
		}
	}
	return nil
}

// writeFlow draws, per card, how the same physical device is labelled and
// linked across the layers: hardware → ALSA (kernel) → PipeWire device → nodes.
// It shows only what is actually wired into the graph — cards that carry at
// least one sink or source — so it stays a map of live connections rather than
// a repeat of the full ALSA CARDS listing. (Idle/suspended endpoints still
// count as wired up; only cards with no endpoint at all are skipped.)
func writeFlow(b *strings.Builder, s style, f model.Facts) {
	section(b, s, "SIGNAL FLOW (wired-up paths only)")
	b.WriteString(s.dim("  Only components linked into the graph. Read top→bottom: kernel up to PipeWire.") + "\n")

	shown := 0
	for _, c := range f.ALSA.Cards {
		nodes := taggedNodes(f, c.Index)
		if len(nodes) == 0 {
			continue // nothing wired through this card; it's only in ALSA CARDS
		}
		shown++

		fmt.Fprintf(b, "\n  card %d  «%s»  id=%s\n", c.Index, cardName(c), c.ID)
		fmt.Fprintf(b, "    hardware │ %s · driver %s · codec %s\n",
			orDash(c.PCI), orDash(c.Driver), orDash(strings.Join(c.Codecs, ", ")))

		if dev := findDeviceForCard(f.PipeWire.Devices, c.Index); dev != nil {
			fmt.Fprintf(b, "    PipeWire │ device %d  «%s»\n", dev.ID, deviceLabel(*dev))
		} else {
			b.WriteString("    PipeWire │ (no device object)\n")
		}
		for _, tn := range nodes {
			writeFlowNode(b, tn.kind, tn.node, f)
		}
	}

	if shown == 0 {
		b.WriteString("\n  (nothing wired up — no active sinks or sources)\n")
	}
}

type taggedNode struct {
	kind string // "sink" or "source"
	node model.Node
}

func taggedNodes(f model.Facts, card int) []taggedNode {
	var out []taggedNode
	for _, n := range nodesForCard(f.PipeWire.Sinks, card) {
		out = append(out, taggedNode{"sink", n})
	}
	for _, n := range nodesForCard(f.PipeWire.Sources, card) {
		out = append(out, taggedNode{"source", n})
	}
	return out
}

func writeFlowNode(b *strings.Builder, kind string, n model.Node, f model.Facts) {
	def := ""
	if n.IsDefault {
		def = "  ★default"
	}
	fmt.Fprintf(b, "             │   └─ %s id %d%s\n", kind, n.ID, def)
	fmt.Fprintf(b, "             │        label   «%s» · %s · %s\n",
		nodeLabel(n), volStr(n), orDash(n.State))
	if n.AlsaPath != "" {
		fmt.Fprintf(b, "             │        opens   %s\n", connectorNote(n.AlsaPath, f))
	}
	if n.AlsaName != "" {
		fmt.Fprintf(b, "             │        ALSA reports endpoint: «%s»\n", n.AlsaName)
	}
	if kind == "sink" {
		flag := " (loopback capture of this output)"
		if monitorIsDefault(f, n) {
			flag = "  ★default input (loopback capture of this output)"
		}
		fmt.Fprintf(b, "             │        monitor «%s.monitor»%s\n", n.Name, flag)
	}
}

// connectorNote decodes an ALSA path and, for HDMI, explains the connector index
// vs the one-based label PipeWire shows in the node name.
func connectorNote(path string, f model.Facts) string {
	p := parseAlsaPath(path)
	if p.Card < 0 {
		return path
	}
	// Name-only paths like "front:2" or "hw:2" carry a card but no device
	// number; resolve the card to its human name so the open-path ties back to
	// the ALSA CARDS / PIPEWIRE DEVICES listings.
	if p.Device < 0 {
		if c := findCard(f.ALSA.Cards, p.Card); c != nil {
			return fmt.Sprintf("%s  →  card %d, %s", path, p.Card, cardName(*c))
		}
		return fmt.Sprintf("%s  →  card %d", path, p.Card)
	}
	base := fmt.Sprintf("%s  →  card %d, ", path, p.Card)
	if p.PCM == "hdmi" {
		return base + fmt.Sprintf("HDMI connector index %d (PipeWire shows this one-based as \"HDMI %d\")",
			p.Device, p.Device+1)
	}
	return base + fmt.Sprintf("PCM device %d", p.Device)
}

// alsaPath holds the parsed parts of a path like "hdmi:1,3" or "hw:1,3,0".
type alsaPath struct {
	PCM       string
	Card      int
	Device    int
	Subdevice int
}

func parseAlsaPath(s string) alsaPath {
	p := alsaPath{Card: -1, Device: -1, Subdevice: -1}
	nums := s
	if i := strings.IndexByte(s, ':'); i >= 0 {
		p.PCM = s[:i]
		nums = s[i+1:]
	}
	parts := strings.Split(nums, ",")
	dst := []*int{&p.Card, &p.Device, &p.Subdevice}
	for i := 0; i < len(parts) && i < len(dst); i++ {
		if v, err := strconv.Atoi(parts[i]); err == nil {
			*dst[i] = v
		}
	}
	return p
}

func findDeviceForCard(devs []model.Device, card int) *model.Device {
	if card < 0 {
		return nil
	}
	for i := range devs {
		if devs[i].AlsaCard == card {
			return &devs[i]
		}
	}
	return nil
}

func findCard(cards []model.Card, index int) *model.Card {
	if index < 0 {
		return nil
	}
	for i := range cards {
		if cards[i].Index == index {
			return &cards[i]
		}
	}
	return nil
}

func writeNotes(b *strings.Builder, s style, f model.Facts) {
	var notes []string
	if f.Procs.PulseAudio != nil && (f.Procs.PipeWire != nil || f.PipeWire.Running) {
		notes = append(notes, s.warn("Both a real PulseAudio daemon and PipeWire are running — "+
			"a conflicting setup."))
	}
	if len(f.MissingTools) > 0 {
		notes = append(notes, "Helper tools not found ("+strings.Join(f.MissingTools, ", ")+
			"); some detail was read directly from the kernel instead.")
	}
	if len(notes) == 0 {
		return
	}
	section(b, s, "NOTES")
	for _, n := range notes {
		b.WriteString("  • " + n + "\n")
	}
}

// ─── glossary ───────────────────────────────────────────────────────────────

type term struct{ name, def string }

func writeGlossary(b *strings.Builder, s style, f model.Facts) {
	section(b, s, "GLOSSARY")

	groups := []struct {
		head  string
		terms []term
	}{
		{"The layers (bottom to top)", []term{
			{"ALSA", "Advanced Linux Sound Architecture — the kernel sound layer; talks to the hardware. Source: /proc/asound."},
			{"PipeWire", "The modern user-space audio/video server that all apps connect to; routes streams to/from devices."},
			{"WirePlumber", "PipeWire's session manager: applies policy, picks defaults, restores volumes/routing."},
			{"pipewire-pulse", "A shim that speaks the PulseAudio protocol on top of PipeWire, so PulseAudio apps work unchanged."},
			{"PulseAudio", "The previous-generation sound server. Here it is only emulated by pipewire-pulse, not run standalone."},
			{"JACK", "JACK Audio Connection Kit — a low-latency pro-audio API; PipeWire can emulate it via pw-jack."},
		}},
		{"Hardware terms", []term{
			{"card", "One sound card/controller the kernel found (e.g. onboard audio, or a GPU's HDMI audio block)."},
			{"PCM", "Pulse-Code Modulation — here, one playback or capture device on a card (the 'device' in card,device)."},
			{"codec", "Coder/decoder chip on the card that converts between analog and digital audio (e.g. Realtek ALC892)."},
			{"HDA", "High Definition Audio — Intel's standard for the on-board audio controller (driver: snd_hda_intel)."},
			{"PCH", "Platform Controller Hub — the Intel motherboard chipset the onboard audio hangs off."},
			{"HDMI/DP", "HDMI / DisplayPort — digital video connectors that also carry audio, usually from the graphics card."},
			{"PCI", "The bus address of the device, e.g. 0000:00:1f.3 (domain:bus:slot.function)."},
			{"DAC/ADC", "Digital-to-Analog / Analog-to-Digital Converter — the analog side of the codec."},
		}},
		{"PipeWire terms", []term{
			{"device", "A PipeWire object for a whole sound device — usually one per ALSA card."},
			{"node", "A PipeWire endpoint in the audio graph: a single sink or source."},
			{"sink", "An output — where audio plays out to (speakers, headphones, HDMI)."},
			{"source", "An input — where audio is captured from (microphone, line-in, a sink's monitor)."},
			{"monitor", "An automatic loopback source paired with every sink (named <sink>.monitor); captures whatever that sink plays. Used to record system audio. When no real input is set, it becomes the default source."},
			{"default", "The sink/source apps use unless told otherwise (marked * above)."},
			{"vol", "Volume, as a percentage of the node's set level (can exceed 100% if boosted)."},
		}},
		{"Device-name notation (e.g. hdmi:1,3)", []term{
			{"name:", "Text before the colon is the ALSA PCM name — how the device is opened (hw=raw, hdmi=HDMI, plughw=auto-convert, front/surround51=channel layout)."},
			{"card", "First number = which card (matches the card numbers above)."},
			{"device", "Second number = which PCM on that card (one card can have many)."},
			{"subdevice", "Optional third number = an individual stream within that PCM (usually 0)."},
			{`"HDMI N"`, "Two unrelated schemes share this name: in /proc/asound/pcm it is the kernel's PCM device label; in a PipeWire sink name (e.g. \"HDMI 4\") it is the connector/port index shown one-based. The path's hdmi:CARD,N uses the PipeWire connector index, NOT the kernel PCM number — so they need not match."},
		}},
		{"Node states", []term{
			{"running", "Audio is actively flowing through the node right now."},
			{"idle", "Open and ready, but nothing is streaming at the moment."},
			{"suspended", "Parked to save power; wakes automatically when an app plays/records. Still available."},
		}},
		{"Status labels", []term{
			{"ACTIVE", "The card/device currently backs at least one sink or source."},
			{"IDLE", "Present and recognised, but no sink/source is active on it right now."},
		}},
	}

	for _, g := range groups {
		b.WriteString("  " + s.dim(g.head) + "\n")
		rows := [][]string{}
		for _, t := range g.terms {
			rows = append(rows, []string{t.name, "— " + t.def})
		}
		b.WriteString(tabBlock("    ", rows))
		b.WriteString("\n")
	}
}

// ─── small helpers ────────────────────────────────────────────────────────────

func cardActive(f model.Facts, index int) bool {
	return len(nodesForCard(f.PipeWire.Sinks, index))+len(nodesForCard(f.PipeWire.Sources, index)) > 0
}

func pcmDir(p model.PCM) string {
	switch {
	case p.Playback && p.Capture:
		return "playback + capture"
	case p.Playback:
		return "playback"
	case p.Capture:
		return "capture"
	default:
		return "-"
	}
}

func nodeLabel(n model.Node) string {
	if n.Description != "" {
		return n.Description
	}
	return n.Name
}

func cardName(c model.Card) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}

func deviceLabel(d model.Device) string {
	if d.Description != "" {
		return d.Description
	}
	if d.Nick != "" {
		return d.Nick
	}
	return "device " + strconv.Itoa(d.ID)
}

func volStr(n model.Node) string {
	if !n.HasVolume {
		return "-"
	}
	v := strconv.Itoa(n.VolumePct) + "%"
	if n.VolumePct > 100 {
		v += "!"
	}
	return v
}

func defaultStr(n *model.Node) string {
	if n == nil {
		return "(none configured)"
	}
	return fmt.Sprintf("id %d  %s", n.ID, nodeLabel(*n))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func nodesForCard(nodes []model.Node, index int) []model.Node {
	if index < 0 {
		return nil
	}
	var out []model.Node
	for _, n := range nodes {
		if n.AlsaCard == index {
			out = append(out, n)
		}
	}
	return out
}

func findDefault(nodes []model.Node, name string) *model.Node {
	if name == "" {
		return nil
	}
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}
