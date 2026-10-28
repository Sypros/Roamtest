//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
)

// dataDir keeps everything in one folder: %LOCALAPPDATA%\RoamTest
func dataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "RoamTest")
}

func runUI(url, dir string) {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(dir, "webview"), // otherwise WebView2 creates a folder next to the exe
		WindowOptions: webview2.WindowOptions{
			Title:  "RoamTest",
			Width:  1440,
			Height: 920,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		messageBox("RoamTest needs the Microsoft Edge WebView2 Runtime.\n\n" +
			"It is part of Windows 10 and 11; if it is missing, install it from Microsoft (search: WebView2 Runtime).")
		return
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
}

func alreadyRunning() {
	messageBox("RoamTest is already running.\n\nLook for its window in the taskbar.")
}

func fatal(msg string) {
	messageBox(msg)
	os.Exit(1)
}

func messageBox(text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	mb := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString("RoamTest")
	mb.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x40) // MB_ICONINFORMATION
}
