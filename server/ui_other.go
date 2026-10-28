//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
)

func dataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "RoamTest")
}

// runUI opens the dashboard in the default browser and waits for Ctrl+C.
func runUI(url, dir string) {
	fmt.Println("RoamTest is running.")
	fmt.Println("Dashboard:", url)
	fmt.Println("Data:     ", dir)
	fmt.Println("Press Ctrl+C to stop.")
	if os.Getenv("ROAMTEST_NO_BROWSER") == "" {
		switch runtime.GOOS {
		case "darwin":
			exec.Command("open", url).Start()
		default:
			exec.Command("xdg-open", url).Start()
		}
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func alreadyRunning() {
	fmt.Println("RoamTest is already running (port 8080 is in use).")
}

func fatal(msg string) {
	fmt.Println(msg)
	os.Exit(1)
}
