package collect

import (
	"bufio"
	"math"
	"regexp"
	"strconv"
	"strings"

	"soundbuddy/internal/model"
	"soundbuddy/internal/run"
)

var (
	// "PipeWire 'pipewire-0' [1.6.6, james@host, cookie:...]"
	wpHeaderRe = regexp.MustCompile(`PipeWire\s+'[^']*'\s+\[([0-9][0-9.]*)`)
	// A device line: "*   50. Some Name [vol: 0.40]" (the * marks the default).
	wpEntryRe = regexp.MustCompile(`(\*?)\s*(\d+)\.\s+.*\[vol:\s*([0-9.]+)`)
)

// WirePlumber runs `wpctl status` to learn the session manager's view. It
// returns the parsed WirePlumber facts, the PipeWire version it reports in its
// header (often the most reliable source), a map of node-id -> volume percent,
// and the helper name if wpctl was missing.
func WirePlumber() (wp model.WirePlumber, pwVersion string, vols map[int]int, missing string) {
	vols = map[int]int{}

	res := run.Command("wpctl", "status")
	if res.Missing() {
		return wp, "", vols, "wpctl"
	}
	if !res.OK() {
		return wp, "", vols, ""
	}
	wp.Running = true

	sc := bufio.NewScanner(strings.NewReader(res.Stdout))
	for sc.Scan() {
		line := sc.Text()

		if pwVersion == "" {
			if m := wpHeaderRe.FindStringSubmatch(line); m != nil {
				pwVersion = m[1]
				wp.Version = m[1]
			}
		}

		if m := wpEntryRe.FindStringSubmatch(line); m != nil {
			id, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			if v, err := strconv.ParseFloat(m[3], 64); err == nil {
				vols[id] = int(math.Round(v * 100))
			}
		}
	}

	return wp, pwVersion, vols, ""
}
