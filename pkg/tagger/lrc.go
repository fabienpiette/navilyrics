package tagger

import (
	"regexp"
	"strconv"
	"strings"
)

type syncedLine struct {
	ms   uint32
	text string
}

// lrcLineRe matches the first LRC timestamp on a line: [mm:ss.xx] or [mm:ss.xxx]
var lrcLineRe = regexp.MustCompile(`\[(\d+):(\d{2})\.(\d{2,3})\]`)

// parseLRC returns timestamped lines from an LRC string.
// Lines without a valid timestamp (e.g. metadata like [ti:...]) are skipped.
// Multiple timestamps on a single line (e.g. [00:01.00][00:02.00] text) are
// each emitted as separate entries with the same text.
func parseLRC(lrc string) []syncedLine {
	var out []syncedLine
	for _, raw := range strings.Split(lrc, "\n") {
		line := strings.TrimSpace(raw)
		matches := lrcLineRe.FindAllStringSubmatchIndex(line, -1)
		if len(matches) == 0 {
			continue
		}
		// text follows all timestamp tags
		lastEnd := matches[len(matches)-1][1]
		text := strings.TrimSpace(line[lastEnd:])

		for _, m := range matches {
			mm, _ := strconv.Atoi(line[m[2]:m[3]])
			ss, _ := strconv.Atoi(line[m[4]:m[5]])
			frac := line[m[6]:m[7]]
			var fracMs int
			switch len(frac) {
			case 2:
				f, _ := strconv.Atoi(frac)
				fracMs = f * 10
			case 3:
				f, _ := strconv.Atoi(frac)
				fracMs = f
			}
			ms := mm*60*1000 + ss*1000 + fracMs
			out = append(out, syncedLine{ms: uint32(ms), text: text})
		}
	}
	return out
}
