// roamtest laptop server: UDP echo for the phone's simulated call, plus a
// local web dashboard that stores every uploaded run and compares them.
package main

import (
	"strings"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

//go:embed dashboard.html
var dashboard []byte

const (
	udpPort  = 5005
	httpPort = 8080
)

var runsDir string

func main() {
	exe, _ := os.Executable()
	runsDir = filepath.Join(filepath.Dir(exe), "roamtest-runs")
	os.MkdirAll(runsDir, 0755)

	go echo()
	go discovery()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboard)
	})
	http.HandleFunc("/upload", upload)
	http.HandleFunc("/api/runs", listRuns)
	http.HandleFunc("/api/run", getRun)
	http.HandleFunc("/api/delete", deleteRun)
	http.HandleFunc("/api/baseline", baselineHandler)

	fmt.Println("==============================================")
	fmt.Println(" RoamTest server")
	fmt.Println("==============================================")
	fmt.Println(" Use one of these IPs on the phone:")
	for _, ip := range localIPs() {
		fmt.Println("    ", ip)
	}
	fmt.Printf("\n Phone app finds this laptop automatically (Find button).\n Termux tool:    ./roamtest <ip> <label>\n")
	fmt.Printf(" Dashboard:      http://localhost:%d\n", httpPort)
	fmt.Printf(" Runs saved in:  %s\n", runsDir)
	fmt.Println("\n If the phone gets no replies, allow this program")
	fmt.Println(" through the firewall (UDP 5005, TCP 8080).")
	fmt.Println("==============================================")

	go func() {
		time.Sleep(700 * time.Millisecond)
		openBrowser(fmt.Sprintf("http://localhost:%d", httpPort))
	}()

	if err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil); err != nil {
		fmt.Println("Web server error:", err)
		fmt.Println("Press Enter to exit")
		fmt.Scanln()
	}
}

func echo() {
	pc, err := net.ListenPacket("udp", fmt.Sprintf(":%d", udpPort))
	if err != nil {
		fmt.Println("Cannot listen on UDP", udpPort, err)
		return
	}
	buf := make([]byte, 2048)
	var last time.Time
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		pc.WriteTo(buf[:n], addr)
		if time.Since(last) > 30*time.Second {
			fmt.Printf("[%s] receiving test traffic from %s\n", time.Now().Format("15:04:05"), addr)
		}
		last = time.Now()
	}
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var run struct {
		Summary struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"summary"`
	}
	if json.Unmarshal(body, &run) != nil || run.Summary.ID == "" {
		http.Error(w, "bad run data", 400)
		return
	}
	id := filepath.Base(run.Summary.ID)
	os.WriteFile(filepath.Join(runsDir, id+".json"), body, 0644)
	fmt.Printf("[%s] run saved: %s\n", time.Now().Format("15:04:05"), run.Summary.Label)
	w.Write([]byte("ok"))
}

func listRuns(w http.ResponseWriter, r *http.Request) {
	files, _ := filepath.Glob(filepath.Join(runsDir, "*.json"))
	sort.Strings(files)
	var out []json.RawMessage
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
		b, _ := json.Marshal(run.Summary)
		out = append(out, b)
	}
	w.Header().Set("Content-Type", "application/json")
	if out == nil {
		w.Write([]byte("[]"))
		return
	}
	json.NewEncoder(w).Encode(out)
}

func getRun(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(r.URL.Query().Get("id"))
	data, err := os.ReadFile(filepath.Join(runsDir, id+".json"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func deleteRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	id := filepath.Base(r.URL.Query().Get("id"))
	os.Rename(filepath.Join(runsDir, id+".json"), filepath.Join(runsDir, id+".json.deleted"))
	w.Write([]byte("ok"))
}

func localIPs() []string {
	var ips []string
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				ips = append(ips, fmt.Sprintf("%-16s (%s)", n.IP.String(), i.Name))
			}
		}
	}
	return ips
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
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

func baselineHandler(w http.ResponseWriter, r *http.Request) {
	f := filepath.Join(runsDir, "baseline.txt")
	if r.Method == http.MethodPost {
		os.WriteFile(f, []byte(filepath.Base(r.URL.Query().Get("id"))), 0644)
	}
	b, _ := os.ReadFile(f)
	w.Write(b)
}

// discovery answers the phone app's "ROAMTEST?" broadcast so nobody has to type an IP.
func discovery() {
	pc, err := net.ListenPacket("udp4", ":5006")
	if err != nil {
		fmt.Println("Discovery unavailable (UDP 5006):", err)
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
			fmt.Printf("[%s] phone %s found this laptop\n", time.Now().Format("15:04:05"), addr)
		}
	}
}
