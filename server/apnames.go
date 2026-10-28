package main

import (
	"encoding/csv"
	"regexp"
	"strings"
)

var macRe = regexp.MustCompile(`(?i)\b([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})[-:]([0-9a-f]{2})\b`)

// parseAPList reads text copied from the Omada device list (or a CSV export)
// and returns MAC (lower case, colon separated) -> device name. It copes with
// one row per line (tab separated) and with one cell per line.
func parseAPList(text string) map[string]string {
	if m, _ := parseOmadaCSV(text); len(m) > 0 {
		return m
	}
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

// parseOmadaCSV reads the CSV from Omada's Devices > Export. Columns are found by
// header name, so other controller versions with different columns still work.
// It also returns the site name. Access points on a wireless uplink get "(mesh)".
func parseOmadaCSV(text string) (map[string]string, string) {
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil || len(recs) < 2 {
		return nil, ""
	}
	col := func(names ...string) int {
		for i, h := range recs[0] {
			h = strings.ToUpper(strings.TrimSpace(h))
			for _, n := range names {
				if h == n {
					return i
				}
			}
		}
		return -1
	}
	cName, cMac := col("DEVICE NAME", "NAME"), col("MAC ADDRESS", "MAC")
	cSite, cStatus := col("SITE NAME", "SITE"), col("STATUS")
	if cName < 0 || cMac < 0 {
		return nil, ""
	}
	out := map[string]string{}
	site := ""
	for _, rec := range recs[1:] {
		get := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		m := macRe.FindStringSubmatch(get(cMac))
		name := get(cName)
		if m == nil || name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(get(cStatus)), "wireless") && !strings.Contains(strings.ToLower(name), "mesh") {
			name += " (mesh)"
		}
		out[strings.ToLower(strings.Join(m[1:7], ":"))] = name
		if site == "" {
			site = get(cSite)
		}
	}
	return out, site
}
