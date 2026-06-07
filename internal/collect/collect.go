// Package collect gathers raw audio-system facts from the kernel and the
// userspace helper tools. Each collector is independent; All() runs them and
// reconciles the few pieces that come from more than one source.
package collect

import (
	"os"

	"soundbuddy/internal/model"
)

// All gathers a complete snapshot of the audio stack.
func All() model.Facts {
	var f model.Facts

	f.Host, _ = os.Hostname()
	f.Procs = Procs()
	f.ALSA = ALSA()

	pw, pwMissing := PipeWire()
	wp, pwVersion, vols, wpMissing := WirePlumber()

	// Version reconciliation: pw-dump rarely exposes core.version, but wpctl's
	// header reliably does, so fall back to it.
	if pw.Version == "" {
		pw.Version = pwVersion
	}

	// Apply per-node volumes (from wpctl) and mark defaults (from pw-dump
	// metadata) onto the node lists.
	applyVolumes(pw.Sinks, vols)
	applyVolumes(pw.Sources, vols)
	markDefault(pw.Sinks, pw.DefaultSinkName)
	markDefault(pw.Sources, pw.DefaultSourceName)

	f.PipeWire = pw
	f.WirePlumber = wp

	for _, m := range []string{pwMissing, wpMissing} {
		if m != "" {
			f.MissingTools = append(f.MissingTools, m)
		}
	}

	return f
}

func applyVolumes(nodes []model.Node, vols map[int]int) {
	for i := range nodes {
		if v, ok := vols[nodes[i].ID]; ok {
			nodes[i].VolumePct = v
			nodes[i].HasVolume = true
		}
	}
}

func markDefault(nodes []model.Node, name string) {
	if name == "" {
		return
	}
	for i := range nodes {
		if nodes[i].Name == name {
			nodes[i].IsDefault = true
		}
	}
}
