package collect

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"soundbuddy/internal/model"
)

// ALSA reads the kernel sound layer directly from /proc/asound and /sys. No
// external binary is involved, so this is the one collector that always works.
func ALSA() model.ALSA {
	var a model.ALSA

	if v, err := os.ReadFile("/proc/asound/version"); err == nil {
		a.Version = parseALSAVersion(string(v))
	}

	cardsData, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return a // /proc/asound absent: leave Read=false
	}
	a.Read = true
	a.Cards = parseCards(string(cardsData))

	// Enrich each card with its PCM devices, codecs, driver and PCI address.
	pcms := parsePCMs()
	for i := range a.Cards {
		c := &a.Cards[i]
		c.PCMs = pcms[c.Index]
		c.Codecs = readCodecs(c.Index)
		c.Driver, c.PCI = readSysfs(c.Index)
	}
	return a
}

// parsePCMs reads /proc/asound/pcm, a flat list of every PCM device across all
// cards. Each line looks like:
//
//	00-03: HDMI 0 : HDMI 0 : playback 1
//	00-00: ALC892 Analog : ALC892 Analog : playback 1 : capture 1
//
// The leading "CC-DD" is card,device. It returns a map of card-index -> PCMs.
func parsePCMs() map[int][]model.PCM {
	out := map[int][]model.PCM{}
	data, err := os.ReadFile("/proc/asound/pcm")
	if err != nil {
		return out
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 2 {
			continue
		}
		ids := strings.SplitN(strings.TrimSpace(fields[0]), "-", 2)
		if len(ids) != 2 {
			continue
		}
		card, err1 := strconv.Atoi(ids[0])
		dev, err2 := strconv.Atoi(ids[1])
		if err1 != nil || err2 != nil {
			continue
		}
		p := model.PCM{Device: dev, Name: strings.TrimSpace(fields[1])}
		rest := sc.Text()
		p.Playback = strings.Contains(rest, "playback")
		p.Capture = strings.Contains(rest, "capture")
		out[card] = append(out[card], p)
	}
	return out
}

// readCodecs pulls the "Codec:" line from each /proc/asound/cardN/codec#* file.
func readCodecs(card int) []string {
	var codecs []string
	matches, _ := filepath.Glob(fmt.Sprintf("/proc/asound/card%d/codec#*", card))
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "Codec: "); ok {
				codecs = append(codecs, strings.TrimSpace(name))
				break
			}
		}
	}
	return codecs
}

// readSysfs resolves the card's kernel driver and PCI slot from
// /sys/class/sound/cardN. Both are best-effort; missing values come back empty.
func readSysfs(card int) (driver, pci string) {
	base := fmt.Sprintf("/sys/class/sound/card%d/device", card)
	if dev, err := filepath.EvalSymlinks(base); err == nil {
		name := filepath.Base(dev)
		// A PCI slot looks like "0000:00:1f.3"; skip platform/usb names.
		if strings.Count(name, ":") == 2 {
			pci = name
		}
	}
	if drv, err := filepath.EvalSymlinks(base + "/driver"); err == nil {
		driver = filepath.Base(drv)
	}
	return driver, pci
}

// parseALSAVersion pulls the trailing version token out of e.g.
// "Advanced Linux Sound Architecture Driver Version k7.0.11-1-cachyos."
func parseALSAVersion(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	if i := strings.LastIndex(s, "Version "); i >= 0 {
		return strings.TrimSpace(s[i+len("Version "):])
	}
	return s
}

// parseCards parses /proc/asound/cards, which lists each card as a header line
// plus an indented detail line:
//
//	 0 [PCH            ]: HDA-Intel - HDA Intel PCH
//	                      HDA Intel PCH at 0xdff40000 irq 133
func parseCards(s string) []model.Card {
	var cards []model.Card
	var cur *model.Card

	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}

		// Header lines start with a number after optional leading spaces.
		trimmed := strings.TrimLeft(line, " ")
		if br := strings.IndexByte(trimmed, '['); br > 0 && isAllDigits(strings.TrimSpace(trimmed[:br])) {
			if cur != nil {
				cards = append(cards, *cur)
			}
			cur = parseCardHeader(trimmed)
			continue
		}

		// Otherwise it's the indented long-name detail for the current card.
		if cur != nil {
			cur.LongName = strings.TrimSpace(line)
		}
	}
	if cur != nil {
		cards = append(cards, *cur)
	}
	return cards
}

func parseCardHeader(s string) *model.Card {
	c := &model.Card{}

	open := strings.IndexByte(s, '[')
	close := strings.IndexByte(s, ']')
	if open < 0 || close < 0 || close < open {
		return c
	}
	c.Index, _ = strconv.Atoi(strings.TrimSpace(s[:open]))
	c.ID = strings.TrimSpace(s[open+1 : close])

	// After "]: " comes "driver - name"; we keep the name half.
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s[close+1:]), ":"))
	if i := strings.Index(rest, " - "); i >= 0 {
		c.Name = strings.TrimSpace(rest[i+3:])
	} else {
		c.Name = rest
	}
	return c
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
