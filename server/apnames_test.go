package main

import "testing"

func TestParseAPList(t *testing.T) {
	tab := "Device Name\tMAC Address\tStatus\nGelijkvloers - Keuken\tA8-29-48-F0-03-80\tConnected\nTerras - Outdoor achter struiken\tD4-D6-DF-01-2A-30\tConnected\n"
	cells := "Device Name\nMAC Address\nGelijkvloers - Bureel\nA8-29-48-F8-BB-A0\nConnected\nGelijkvloers - Leefruimte (Mesh)\nA8-29-48-CB-FA-B0\n"
	csv := "\"Name\",\"MAC\"\n\"Kelder - Garage\",\"A8:29:48:F8:BA:E0\"\n"
	omada := "DEVICE NAME,SITE NAME,MAC ADDRESS,IP ADDRESS,STATUS\n" +
		"\"Gelijkvloers - Keuken\",\"Champel\",\"A8-29-48-F0-03-80\",\"192.168.0.112\",\"Connected\"\n" +
		"\"Leefruimte\",\"Champel\",\"A8-29-48-CB-FA-B0\",\"192.168.0.158\",\"Connected(Wireless)\"\n"
	if m, site := parseOmadaCSV(omada); m["a8:29:48:cb:fa:b0"] != "Leefruimte (mesh)" || site != "Champel" || len(m) != 2 {
		t.Errorf("omada csv: %v %q", m, site)
	}
	for name, c := range map[string]struct {
		in   string
		mac  string
		want string
		n    int
	}{
		"tab":   {tab, "a8:29:48:f0:03:80", "Gelijkvloers - Keuken", 2},
		"cells": {cells, "a8:29:48:cb:fa:b0", "Gelijkvloers - Leefruimte (Mesh)", 2},
		"csv":   {csv, "a8:29:48:f8:ba:e0", "Kelder - Garage", 1},
	} {
		got := parseAPList(c.in)
		if got[c.mac] != c.want || len(got) != c.n {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
