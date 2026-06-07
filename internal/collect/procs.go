package collect

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"soundbuddy/internal/model"
)

// procNames maps the comm value (kernel's process name, truncated to 15 chars)
// to the daemon we record it as.
//
// Note: /proc/<pid>/comm is truncated at 15 characters, but every name we look
// for fits ("pipewire-pulse" is 14, "wireplumber" 11), so an exact match is safe.

// Procs scans /proc for the audio daemons we care about. It is pure file reading
// — no external command — so it works even on a box stripped of helper tools.
func Procs() model.Procs {
	var p model.Procs

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return p
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid directory
		}
		commBytes, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue // process vanished or not ours to read
		}
		comm := strings.TrimSpace(string(commBytes))
		proc := &model.Process{Name: comm, PID: pid}

		switch comm {
		case "pipewire":
			if p.PipeWire == nil {
				p.PipeWire = proc
			}
		case "pipewire-pulse":
			if p.PipeWirePulse == nil {
				p.PipeWirePulse = proc
			}
		case "wireplumber":
			if p.WirePlumber == nil {
				p.WirePlumber = proc
			}
		case "pulseaudio":
			if p.PulseAudio == nil {
				p.PulseAudio = proc
			}
		case "jackd", "jackdbus":
			if p.JACK == nil {
				p.JACK = proc
			}
		}
	}

	return p
}
