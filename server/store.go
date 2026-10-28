package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// All data lives under one folder (dataDir):
//
//	projects/<id>/project.json   name, created, baseline
//	projects/<id>/runs/*.json    one file per walk
//	current.txt                  id of the open project
//	webview/                     window engine cache (Windows only)

type Project struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Created  string `json:"created"`
	Baseline string `json:"baseline"`
	Runs     int    `json:"runs"`
}

type store struct {
	mu      sync.Mutex
	dir     string
	current string
	version int64
}

func newStore(dir, legacyDir string) *store {
	s := &store{dir: dir}
	os.MkdirAll(filepath.Join(dir, "projects"), 0755)
	if b, err := os.ReadFile(filepath.Join(dir, "current.txt")); err == nil {
		s.current = strings.TrimSpace(string(b))
	}
	if _, err := os.Stat(s.projectFile(s.current)); s.current == "" || err != nil {
		list := s.projects()
		if len(list) > 0 {
			s.current = list[len(list)-1].ID
		} else if legacyDir != "" && s.importLegacy(legacyDir) {
			// s.current set by importLegacy
		} else {
			p := s.create("First project")
			s.current = p.ID
		}
		s.saveCurrent()
	}
	s.version = time.Now().Unix() * 1000 // fits in a JavaScript number
	return s
}

func (s *store) bump() { s.version++ }

func (s *store) projectDir(id string) string {
	return filepath.Join(s.dir, "projects", filepath.Base(id))
}
func (s *store) projectFile(id string) string { return filepath.Join(s.projectDir(id), "project.json") }
func (s *store) runsDir(id string) string     { return filepath.Join(s.projectDir(id), "runs") }
func (s *store) saveCurrent() {
	os.WriteFile(filepath.Join(s.dir, "current.txt"), []byte(s.current), 0644)
}

func (s *store) load(id string) (Project, bool) {
	var p Project
	b, err := os.ReadFile(s.projectFile(id))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return p, false
	}
	p.ID = id
	files, _ := filepath.Glob(filepath.Join(s.runsDir(id), "*.json"))
	p.Runs = len(files)
	return p, true
}

func (s *store) save(p Project) {
	os.MkdirAll(s.runsDir(p.ID), 0755)
	b, _ := json.MarshalIndent(p, "", "  ")
	os.WriteFile(s.projectFile(p.ID), b, 0644)
}

func (s *store) projects() []Project {
	dirs, _ := os.ReadDir(filepath.Join(s.dir, "projects"))
	var out []Project
	for _, d := range dirs {
		if p, ok := s.load(d.Name()); ok && d.IsDir() {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

func (s *store) create(name string) Project {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "New project"
	}
	id := time.Now().Format("20060102_150405") + "_" + slug(name)
	p := Project{ID: id, Name: name, Created: time.Now().Format("2006-01-02 15:04:05")}
	s.save(p)
	return p
}

// importLegacy moves runs from the old "roamtest-runs" folder next to the exe into a project.
func (s *store) importLegacy(oldDir string) bool {
	files, _ := filepath.Glob(filepath.Join(oldDir, "*.json"))
	if len(files) == 0 {
		return false
	}
	p := s.create("Imported runs")
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			os.WriteFile(filepath.Join(s.runsDir(p.ID), filepath.Base(f)), b, 0644)
		}
	}
	if b, err := os.ReadFile(filepath.Join(oldDir, "baseline.txt")); err == nil {
		p.Baseline = strings.TrimSpace(string(b))
		s.save(p)
	}
	s.current = p.ID
	s.saveCurrent()
	// leave a note so the old folder can be deleted safely
	os.WriteFile(filepath.Join(oldDir, "IMPORTED - you can delete this folder.txt"),
		[]byte("These runs were copied into RoamTest project \"Imported runs\".\r\n"), 0644)
	return true
}

// runSummaries returns the summary of every run in a project, plus derived band hops.
func (s *store) runSummaries(id string) []map[string]any {
	files, _ := filepath.Glob(filepath.Join(s.runsDir(id), "*.json"))
	sort.Strings(files)
	out := []map[string]any{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var run struct {
			Summary map[string]any `json:"summary"`
			Rows    []struct {
				Freq  int    `json:"freq"`
				Event string `json:"event"`
			} `json:"rows"`
		}
		if json.Unmarshal(data, &run) != nil || run.Summary == nil {
			continue
		}
		hops := 0
		for i := 1; i < len(run.Rows); i++ {
			if run.Rows[i].Event != "" && band(run.Rows[i].Freq) != band(run.Rows[i-1].Freq) {
				hops++
			}
		}
		run.Summary["band_hops"] = hops
		out = append(out, run.Summary)
	}
	return out
}

// export builds a zip: report.html (opens anywhere, no install), runs.csv and the raw runs.
func (s *store) export(id string, page []byte) ([]byte, string, error) {
	p, ok := s.load(id)
	if !ok {
		return nil, "", fmt.Errorf("project not found")
	}
	sums := s.runSummaries(id)
	full := map[string]json.RawMessage{}
	files, _ := filepath.Glob(filepath.Join(s.runsDir(id), "*.json"))
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			full[strings.TrimSuffix(filepath.Base(f), ".json")] = b
		}
	}
	report := map[string]any{
		"project":  p,
		"runs":     sums,
		"full":     full,
		"exported": time.Now().Format("2006-01-02 15:04"),
	}
	rj, _ := json.Marshal(report)
	rj = bytes.ReplaceAll(rj, []byte("</"), []byte("<\\/"))
	html := bytes.Replace(page, []byte("<!--REPORT-->"),
		append(append([]byte("<script>window.REPORT="), rj...), []byte(";</script>")...), 1)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	base := slug(p.Name)
	add := func(name string, data []byte) {
		w, _ := zw.Create(base + "/" + name)
		w.Write(data)
	}
	add("report.html", html)
	add("runs.csv", csvOf(sums, p.Baseline))
	for name, b := range full {
		add("runs/"+name+".json", b)
	}
	zw.Close()
	fname := fmt.Sprintf("RoamTest_%s_%s.zip", base, time.Now().Format("2006-01-02"))
	return buf.Bytes(), fname, nil
}

func csvOf(sums []map[string]any, baseline string) []byte {
	cols := []string{"label", "date", "device", "duration_s", "roams", "band_hops", "avg_roam_gap_ms",
		"worst_roam_gap_ms", "worst_gap_ms", "bad_seconds", "loss_pct", "median_rssi", "aps", "marks"}
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf") // so Excel reads UTF-8
	b.WriteString(strings.Join(append(cols, "baseline"), ";") + "\r\n")
	for _, s := range sums {
		var row []string
		for _, c := range cols {
			v := s[c]
			str := ""
			if v != nil {
				str = strings.ReplaceAll(fmt.Sprint(v), ";", ",")
				if f, ok := v.(float64); ok {
					str = strings.Replace(fmt.Sprintf("%g", f), ".", ",", 1) // Belgian Excel decimal comma
				}
			}
			row = append(row, str)
		}
		isB := ""
		if fmt.Sprint(s["id"]) == baseline {
			isB = "yes"
		}
		b.WriteString(strings.Join(append(row, isB), ";") + "\r\n")
	}
	return b.Bytes()
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "project"
	}
	return b.String()
}

func readAllLimit(r io.Reader) ([]byte, error) { return io.ReadAll(io.LimitReader(r, 50<<20)) }
