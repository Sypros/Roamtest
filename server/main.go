// RoamTest laptop app: UDP echo for the phone's simulated call, discovery so the
// phone finds the laptop, and a dashboard (own window on Windows) that stores
// every walk per project and compares them.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed dashboard.html
var dashboard []byte

const (
	udpPort       = 5005
	discoveryPort = 5006
	httpPort      = 8080
)

var st *store

// live state of the phone's test traffic
var live struct {
	sync.Mutex
	addr    string
	last    time.Time
	started time.Time
	packets int64
}

func main() {
	dir := dataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		fatal("Cannot create data folder " + dir + ": " + err.Error())
	}
	exe, _ := os.Executable()
	st = newStore(dir, filepath.Join(filepath.Dir(exe), "roamtest-runs"))

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", httpPort))
	if err != nil {
		alreadyRunning()
		return
	}

	go echo()
	go discovery()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(dashboard)
	})
	mux.HandleFunc("/upload", upload)
	mux.HandleFunc("/api/state", apiState)
	mux.HandleFunc("/api/runs", apiRuns)
	mux.HandleFunc("/api/run", apiRun)
	mux.HandleFunc("/api/delete", apiDelete)
	mux.HandleFunc("/api/baseline", apiBaseline)
	mux.HandleFunc("/api/project/new", apiProjectNew)
	mux.HandleFunc("/api/project/open", apiProjectOpen)
	mux.HandleFunc("/api/project/rename", apiProjectRename)
	mux.HandleFunc("/api/project/delete", apiProjectDelete)
	mux.HandleFunc("/api/aps", apiAPs)
	mux.HandleFunc("/api/export", apiExport)
	go http.Serve(ln, mux)

	runUI(fmt.Sprintf("http://localhost:%d/", httpPort), dir)
}

// ------------------------------------------------------------------ network

func echo() {
	pc, err := net.ListenPacket("udp", fmt.Sprintf(":%d", udpPort))
	if err != nil {
		return
	}
	buf := make([]byte, 2048)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		pc.WriteTo(buf[:n], addr)
		now := time.Now()
		live.Lock()
		if now.Sub(live.last) > 3*time.Second {
			live.started = now
			live.packets = 0
		}
		live.addr = addr.(*net.UDPAddr).IP.String()
		live.last = now
		live.packets++
		live.Unlock()
	}
}

// discovery answers the phone app's "ROAMTEST?" broadcast so nobody has to type an IP.
func discovery() {
	pc, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", discoveryPort))
	if err != nil {
		return
	}
	host, _ := os.Hostname()
	buf := make([]byte, 64)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		if strings.HasPrefix(string(buf[:n]), "ROAMTEST?") {
			pc.WriteTo([]byte("ROAMTEST "+host), addr)
		}
	}
}

func localIPs() []map[string]string {
	var ips []map[string]string
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
				ips = append(ips, map[string]string{"ip": n.IP.String(), "iface": i.Name})
			}
		}
	}
	return ips
}

// ---------------------------------------------------------------------- API

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func apiState(w http.ResponseWriter, r *http.Request) {
	st.mu.Lock()
	cur, _ := st.load(st.current)
	list := st.projects()
	ver := st.version
	st.mu.Unlock()

	live.Lock()
	active := time.Since(live.last) < 2*time.Second
	l := map[string]any{"active": active, "phone": live.addr,
		"seconds": 0}
	if active {
		l["seconds"] = int(time.Since(live.started).Seconds())
	}
	if !live.last.IsZero() {
		l["ago"] = int(time.Since(live.last).Seconds())
	}
	live.Unlock()

	host, _ := os.Hostname()
	writeJSON(w, map[string]any{
		"project": cur, "projects": list, "version": ver,
		"live": l, "ips": localIPs(), "host": host, "dataDir": st.dir,
	})
}

func apiRuns(w http.ResponseWriter, r *http.Request) {
	st.mu.Lock()
	defer st.mu.Unlock()
	writeJSON(w, st.runSummaries(st.current))
}

func apiRun(w http.ResponseWriter, r *http.Request) {
	st.mu.Lock()
	dir := st.runsDir(st.current)
	st.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(dir, filepath.Base(r.URL.Query().Get("id"))+".json"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	body, err := readAllLimit(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var run struct {
		Summary struct {
			ID string `json:"id"`
		} `json:"summary"`
	}
	if json.Unmarshal(body, &run) != nil || run.Summary.ID == "" {
		http.Error(w, "bad run data", 400)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	os.MkdirAll(st.runsDir(st.current), 0755)
	os.WriteFile(filepath.Join(st.runsDir(st.current), filepath.Base(run.Summary.ID)+".json"), body, 0644)
	st.bump()
	w.Write([]byte("ok"))
}

func post(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return false
	}
	return true
}

func apiDelete(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	id := filepath.Base(r.URL.Query().Get("id"))
	trash := filepath.Join(st.projectDir(st.current), "deleted")
	os.MkdirAll(trash, 0755)
	os.Rename(filepath.Join(st.runsDir(st.current), id+".json"), filepath.Join(trash, id+".json"))
	st.bump()
	w.Write([]byte("ok"))
}

func apiBaseline(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.load(st.current)
	if ok {
		p.Baseline = filepath.Base(r.URL.Query().Get("id"))
		st.save(p)
		st.bump()
	}
	w.Write([]byte("ok"))
}

func apiProjectNew(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	p := st.create(r.URL.Query().Get("name"))
	st.current = p.ID
	st.saveCurrent()
	st.bump()
	writeJSON(w, p)
}

func apiProjectOpen(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	id := filepath.Base(r.URL.Query().Get("id"))
	if _, ok := st.load(id); ok {
		st.current = id
		st.saveCurrent()
		st.bump()
	}
	w.Write([]byte("ok"))
}

func apiProjectRename(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if p, ok := st.load(st.current); ok {
		if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
			p.Name = name
			st.save(p)
			st.bump()
		}
	}
	w.Write([]byte("ok"))
}

// apiProjectDelete moves the open project to the "deleted" folder inside the
// data folder (recoverable by hand) and opens the most recent other project.
func apiProjectDelete(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	trash := filepath.Join(st.dir, "deleted")
	os.MkdirAll(trash, 0755)
	if err := os.Rename(st.projectDir(st.current), filepath.Join(trash, filepath.Base(st.current))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	list := st.projects()
	if len(list) > 0 {
		st.current = list[len(list)-1].ID
	} else {
		st.current = st.create("New project").ID
	}
	st.saveCurrent()
	st.bump()
	w.Write([]byte("ok"))
}

// apiAPs takes text pasted from the Omada device list and stores MAC -> name
// for the open project.
func apiAPs(w http.ResponseWriter, r *http.Request) {
	if !post(w, r) {
		return
	}
	body, err := readAllLimit(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	aps := parseAPList(string(body))
	_, site := parseOmadaCSV(string(body))
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.load(st.current)
	if !ok {
		http.Error(w, "no project", 404)
		return
	}
	p.APs = aps
	// a fresh project still has its default name: take the Omada site name
	if site != "" && (p.Name == "First project" || p.Name == "New project") {
		p.Name = site
	}
	st.save(p)
	st.bump()
	writeJSON(w, map[string]any{"count": len(aps), "aps": aps, "site": site})
}

func apiExport(w http.ResponseWriter, r *http.Request) {
	st.mu.Lock()
	id := r.URL.Query().Get("id")
	if id == "" {
		id = st.current
	}
	data, name, err := st.export(filepath.Base(id), dashboard)
	st.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(data)
}

// -------------------------------------------------------------------- misc

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func band(f int) string {
	switch {
	case f >= 5925:
		return "6G"
	case f >= 5000:
		return "5G"
	case f > 0:
		return "2.4G"
	}
	return "?"
}
