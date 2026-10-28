// roamtest phone client: simulates a VoIP call (one 160-byte packet every 20 ms)
// to the laptop, logs loss / latency / jitter / audio gaps / AP per second,
// flags every roam, and uploads the run to the laptop dashboard when stopped.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	udpPort  = 5005
	httpPort = 8080
	interval = 20 * time.Millisecond
)

type Row struct {
	T       int     `json:"t"`
	Time    string  `json:"time"`
	BSSID   string  `json:"bssid"`
	RSSI    int     `json:"rssi"`
	Freq    int     `json:"freq"`
	Tx      int     `json:"tx"`
	Rx      int     `json:"rx"`
	Loss    float64 `json:"loss"`
	RTT     float64 `json:"rtt"`
	Jitter  float64 `json:"jitter"`
	Gap     float64 `json:"gap"`
	Event   string  `json:"event"`
}

type Summary struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	Date          string  `json:"date"`
	DurationS     int     `json:"duration_s"`
	LossPct       float64 `json:"loss_pct"`
	Roams         int     `json:"roams"`
	AvgRoamGapMs  float64 `json:"avg_roam_gap_ms"`
	WorstRoamGap  float64 `json:"worst_roam_gap_ms"`
	WorstGapMs    float64 `json:"worst_gap_ms"`
	BadSeconds    int     `json:"bad_seconds"`
	MedianRSSI    float64 `json:"median_rssi"`
	APs           int     `json:"aps"`
}

type Run struct {
	Summary Summary `json:"summary"`
	Rows    []Row   `json:"rows"`
}

var (
	mu      sync.Mutex
	winTx   int
	winRx   int
	winRTT  []float64
	winGap  time.Duration
	totTx   int
	totRx   int
	lastRx  = time.Now()
	start   = time.Now()

	wifiMu sync.Mutex
	wifi   = struct {
		BSSID string `json:"bssid"`
		RSSI  int    `json:"rssi"`
		Freq  int    `json:"frequency_mhz"`
	}{BSSID: "?"}
	wifiOK = true
)

func usage() {
	fmt.Println("Usage: ./roamtest <laptop-ip> <label>")
	fmt.Println("Example: ./roamtest 192.168.1.50 baseline")
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	host := os.Args[1]
	label := "test"
	if len(os.Args) > 2 {
		label = strings.Join(os.Args[2:], "-")
	}

	conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", host, udpPort))
	if err != nil {
		fmt.Println("Cannot open UDP socket:", err)
		os.Exit(1)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// sender: one packet every 20 ms
	wg.Add(1)
	go func() {
		defer wg.Done()
		payload := make([]byte, 172)
		var seq uint32
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				binary.BigEndian.PutUint32(payload[0:4], seq)
				binary.BigEndian.PutUint64(payload[4:12], uint64(time.Now().UnixNano()))
				conn.Write(payload) // errors ignored: network briefly gone during a roam
				mu.Lock()
				winTx++
				totTx++
				mu.Unlock()
				seq++
			}
		}
	}()

	// receiver
	go func() {
		buf := make([]byte, 2048)
		for {
			conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, err := conn.Read(buf)
			select {
			case <-stop:
				return
			default:
			}
			if err != nil || n < 12 {
				continue
			}
			now := time.Now()
			sent := int64(binary.BigEndian.Uint64(buf[4:12]))
			rtt := float64(now.UnixNano()-sent) / 1e6
			mu.Lock()
			winRx++
			totRx++
			winRTT = append(winRTT, rtt)
			if g := now.Sub(lastRx); g > winGap {
				winGap = g
			}
			lastRx = now
			mu.Unlock()
		}
	}()

	// Wi-Fi info poller (Termux:API)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			out, err := exec.Command("termux-wifi-connectioninfo").Output()
			if err == nil {
				var w struct {
					BSSID string `json:"bssid"`
					RSSI  int    `json:"rssi"`
					Freq  int    `json:"frequency_mhz"`
				}
				if json.Unmarshal(out, &w) == nil {
					wifiMu.Lock()
					wifi.BSSID, wifi.RSSI, wifi.Freq = w.BSSID, w.RSSI, w.Freq
					wifiMu.Unlock()
				}
			} else {
				wifiMu.Lock()
				wifiOK = false
				wifiMu.Unlock()
			}
			time.Sleep(time.Second)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	fmt.Printf("\nWalk test '%s' -> %s. Press Ctrl+C (Volume Down + C) to stop.\n\n", label, host)
	fmt.Println("  time      AP (BSSID)          RSSI  band   loss   rtt   jit    gap")

	var rows []Row
	var prev string
	sec := 0
	tick := time.NewTicker(time.Second)
	warned := false

loop:
	for {
		select {
		case <-sig:
			break loop
		case <-tick.C:
			sec++
			now := time.Now()
			mu.Lock()
			tx, rx, rtts, gap := winTx, winRx, winRTT, winGap
			if g := now.Sub(lastRx); g > gap {
				gap = g
			}
			winTx, winRx, winRTT, winGap = 0, 0, nil, 0
			mu.Unlock()

			loss := 0.0
			if tx > 0 {
				loss = math.Max(0, 100*(1-float64(rx)/float64(tx)))
			}
			avg, jit := 0.0, 0.0
			if len(rtts) > 0 {
				for _, r := range rtts {
					avg += r
				}
				avg /= float64(len(rtts))
				for i := 1; i < len(rtts); i++ {
					jit += math.Abs(rtts[i] - rtts[i-1])
				}
				if len(rtts) > 1 {
					jit /= float64(len(rtts) - 1)
				}
			}
			gapMs := float64(gap.Milliseconds())

			wifiMu.Lock()
			b, rssi, freq, ok := wifi.BSSID, wifi.RSSI, wifi.Freq, wifiOK
			wifiMu.Unlock()
			if !ok && !warned {
				fmt.Println("  !! termux-wifi-connectioninfo failed: install Termux:API + 'pkg install termux-api', allow Location")
				warned = true
			}
			ev := ""
			if prev != "" && b != prev && b != "?" {
				ev = "ROAM " + prev + " -> " + b
			}
			if b != "?" {
				prev = b
			}
			r := Row{T: sec, Time: now.Format("15:04:05"), BSSID: b, RSSI: rssi, Freq: freq,
				Tx: tx, Rx: rx, Loss: round1(loss), RTT: round1(avg), Jitter: round1(jit), Gap: gapMs, Event: ev}
			rows = append(rows, r)

			band := "2.4G"
			if freq >= 5000 {
				band = "5G"
			}
			if freq >= 5925 {
				band = "6G"
			}
			flag := ""
			if loss > 5 || gapMs > 150 {
				flag = "  <-- BAD"
			}
			if ev != "" {
				fmt.Printf("  >>> ROAM to %s\n", b)
			}
			fmt.Printf("  %s  %-18s %4d  %-4s %5.1f%% %5.1f %5.1f %5.0fms%s\n",
				r.Time, b, rssi, band, loss, avg, jit, gapMs, flag)
		}
	}
	close(stop)
	tick.Stop()

	run := Run{Summary: summarize(label, rows), Rows: rows}
	s := run.Summary
	fmt.Println("\n=========== SUMMARY ===========")
	fmt.Printf("  Label            %s\n", s.Label)
	fmt.Printf("  Duration         %d s\n", s.DurationS)
	fmt.Printf("  Packet loss      %.2f %%\n", s.LossPct)
	fmt.Printf("  Roams            %d (across %d APs)\n", s.Roams, s.APs)
	fmt.Printf("  Avg roam gap     %.0f ms\n", s.AvgRoamGapMs)
	fmt.Printf("  Worst roam gap   %.0f ms\n", s.WorstRoamGap)
	fmt.Printf("  Worst gap        %.0f ms\n", s.WorstGapMs)
	fmt.Printf("  Bad seconds      %d\n", s.BadSeconds)
	fmt.Printf("  Median RSSI      %.0f dBm\n", s.MedianRSSI)

	data, _ := json.MarshalIndent(run, "", " ")
	local := fmt.Sprintf("roam_%s.json", s.ID)
	os.WriteFile(local, data, 0644)
	fmt.Println("\n  Saved on phone:", local)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(fmt.Sprintf("http://%s:%d/upload", host, httpPort), "application/json", bytes.NewReader(data))
	if err != nil {
		fmt.Println("  Upload to laptop FAILED:", err)
		fmt.Printf("  Retry later with: ./roamtest upload %s %s\n", host, local)
		return
	}
	resp.Body.Close()
	fmt.Printf("  Uploaded. Open http://%s:%d on the laptop to compare.\n", host, httpPort)
}

func summarize(label string, rows []Row) Summary {
	s := Summary{Label: label, Date: start.Format("2006-01-02 15:04"),
		ID: start.Format("20060102_150405") + "_" + safe(label), DurationS: len(rows)}
	tx, rx := 0, 0
	var roamGaps, rssis []float64
	aps := map[string]bool{}
	for i, r := range rows {
		tx += r.Tx
		rx += r.Rx
		if r.Gap > s.WorstGapMs {
			s.WorstGapMs = r.Gap
		}
		if r.Loss > 5 || r.Gap > 150 {
			s.BadSeconds++
		}
		if r.RSSI != 0 {
			rssis = append(rssis, float64(r.RSSI))
		}
		if r.BSSID != "?" && r.BSSID != "" {
			aps[r.BSSID] = true
		}
		if r.Event != "" {
			// a roam's dropout can land in the second before or after the BSSID change
			g := r.Gap
			if i > 0 && rows[i-1].Gap > g {
				g = rows[i-1].Gap
			}
			if i+1 < len(rows) && rows[i+1].Gap > g {
				g = rows[i+1].Gap
			}
			roamGaps = append(roamGaps, g)
		}
	}
	if tx > 0 {
		s.LossPct = round2(math.Max(0, 100*(1-float64(rx)/float64(tx))))
	}
	s.Roams = len(roamGaps)
	for _, g := range roamGaps {
		s.AvgRoamGapMs += g
		if g > s.WorstRoamGap {
			s.WorstRoamGap = g
		}
	}
	if s.Roams > 0 {
		s.AvgRoamGapMs = math.Round(s.AvgRoamGapMs / float64(s.Roams))
	}
	if len(rssis) > 0 {
		sort.Float64s(rssis)
		s.MedianRSSI = rssis[len(rssis)/2]
	}
	s.APs = len(aps)
	return s
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

func init() {
	// ./roamtest upload <laptop-ip> <file.json> : resend a saved run
	if len(os.Args) == 4 && os.Args[1] == "upload" {
		data, err := os.ReadFile(os.Args[3])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		resp, err := http.Post(fmt.Sprintf("http://%s:%d/upload", os.Args[2], httpPort), "application/json", bytes.NewReader(data))
		if err != nil {
			fmt.Println("Upload failed:", err)
			os.Exit(1)
		}
		resp.Body.Close()
		fmt.Println("Uploaded.")
		os.Exit(0)
	}
}
