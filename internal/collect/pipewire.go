package collect

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"

	"soundbuddy/internal/model"
	"soundbuddy/internal/run"
)

// pwObject is the slice of a pw-dump entry we care about. pw-dump emits a JSON
// array of heterogeneous objects; we decode loosely and ignore everything else.
type pwObject struct {
	ID       int    `json:"id"`
	Type     string `json:"type"`
	Info     pwInfo `json:"info"`
	Metadata []pwMetadataEntry `json:"metadata"`
	Props    pwProps `json:"props"` // present on Metadata objects
}

type pwInfo struct {
	Props pwProps `json:"props"`
	State string  `json:"state"`
}

type pwProps struct {
	MediaClass      string  `json:"media.class"`
	NodeName        string  `json:"node.name"`
	NodeDescription string  `json:"node.description"`
	CoreVersion     string  `json:"core.version"`
	MetadataName    string  `json:"metadata.name"`
	AlsaCard        flexInt `json:"alsa.card"`
	AlsaPath        string  `json:"api.alsa.path"`
	AlsaName        string  `json:"alsa.name"`
	DeviceDesc      string  `json:"device.description"`
	DeviceNick      string  `json:"device.nick"`
}

// flexInt decodes an int that pw-dump may encode as either a JSON number or a
// quoted string (SPA props are inconsistent about this). Absent/garbage -> -1.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	*f = -1
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		*f = flexInt(n)
	}
	return nil
}

type pwMetadataEntry struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// PipeWire collects the PipeWire graph via pw-dump (JSON). The second return is
// the name of the helper if it was missing, so the caller can record it.
func PipeWire() (pw model.PipeWire, missing string) {
	pw.HasJACK = hasJACKBridge()

	res := run.Command("pw-dump")
	if res.Missing() {
		return pw, "pw-dump"
	}
	if !res.OK() || res.Stdout == "" {
		return pw, ""
	}

	var objs []pwObject
	if err := json.Unmarshal([]byte(res.Stdout), &objs); err != nil {
		return pw, ""
	}
	pw.Running = true

	for _, o := range objs {
		switch o.Type {
		case "PipeWire:Interface:Core":
			if o.Info.Props.CoreVersion != "" {
				pw.Version = o.Info.Props.CoreVersion
			}
		case "PipeWire:Interface:Node":
			p := o.Info.Props
			n := model.Node{
				ID:          o.ID,
				Name:        p.NodeName,
				Description: p.NodeDescription,
				MediaClass:  p.MediaClass,
				State:       o.Info.State,
				VolumePct:   -1,
				AlsaCard:    int(p.AlsaCard),
				AlsaPath:    p.AlsaPath,
				AlsaName:    p.AlsaName,
			}
			switch p.MediaClass {
			case "Audio/Sink":
				pw.Sinks = append(pw.Sinks, n)
			case "Audio/Source", "Audio/Source/Virtual":
				pw.Sources = append(pw.Sources, n)
			}
		case "PipeWire:Interface:Device":
			p := o.Info.Props
			if p.MediaClass == "Audio/Device" {
				pw.Devices = append(pw.Devices, model.Device{
					ID:          o.ID,
					Description: p.DeviceDesc,
					Nick:        p.DeviceNick,
					AlsaCard:    int(p.AlsaCard),
				})
			}
		case "PipeWire:Interface:Metadata":
			if o.Props.MetadataName == "default" {
				readDefaults(o.Metadata, &pw)
			}
		}
	}
	return pw, ""
}

// defaultRef matches the {"name": "..."} payload of default.audio.sink/source.
type defaultRef struct {
	Name string `json:"name"`
}

func readDefaults(entries []pwMetadataEntry, pw *model.PipeWire) {
	for _, e := range entries {
		var ref defaultRef
		if json.Unmarshal(e.Value, &ref) != nil {
			continue
		}
		switch e.Key {
		case "default.audio.sink":
			pw.DefaultSinkName = ref.Name
		case "default.audio.source":
			pw.DefaultSourceName = ref.Name
		}
	}
}

// hasJACKBridge reports whether PipeWire's JACK shim is installed, i.e. whether
// JACK apps can be run through PipeWire via pw-jack.
func hasJACKBridge() bool {
	_, err := exec.LookPath("pw-jack")
	return err == nil
}
