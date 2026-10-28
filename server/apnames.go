package main

import (
	"regexp"
	"strings"
)

var macRe = regexp.MustCompile(`(?i)\b([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})\b`)

// parseAPList reads text copied from the Omada device list (or a CSV export)
// and returns MAC (lower case, colon separated) -> device name. It copes with
// one row per line (tab separated) and with one cell per line.
func parseAPList(text string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	prev := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		loc := macRe.FindStringSubmatchIndex(line)
		if loc == nil {
			if !isHeader(line) {
				prev = line
			}
			continue
		}
		m := macRe.FindStringSubmatch(line)
		mac := strings.ToLower(strings.Join(m[1:7], ":"))
		name := cleanName(line[:loc[0]])
		if name == "" {
			name = cleanName(prev)
		}
		if name != "" {
			out[mac] = name
		}
		prev = ""
	}
	return out
}

func cleanName(s string) string {
	s = strings.NewReplacer("\t", " ", ";", " ", "\"", "").Replace(s)
	s = strings.Trim(strings.TrimSpace(s), ",")
	return strings.Join(strings.Fields(s), " ")
}

func isHeader(s string) bool {
	l := strings.ToLower(s)
	for _, h := range []string{"device name", "mac address", "status", "model", "ip address"} {
		if l == h {
			return true
		}
	}
	return false
}
