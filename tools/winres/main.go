package main

import (
	"image/png"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	icon, err := winres.NewIconFromResizedImage(img, []int{256, 64, 48, 32, 24, 16})
	if err != nil {
		log.Fatal(err)
	}
	rs := winres.ResourceSet{}
	rs.SetIcon(winres.ID(1), icon)
	vi := version.Info{FileVersion: [4]uint16{2, 0, 0, 0}, ProductVersion: [4]uint16{2, 0, 0, 0}}
	vi.Set(0, version.ProductName, "RoamTest")
	vi.Set(0, version.FileDescription, "RoamTest - Wi-Fi roaming walk test")
	vi.Set(0, version.CompanyName, "RoamTest")
	vi.Set(0, version.ProductVersion, "2.0")
	vi.Set(0, version.FileVersion, "2.0")
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
	})
	out, err := os.Create(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}
