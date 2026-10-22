package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"golang.org/x/image/draw"
)

// TestAppIcon writes resources/icon.png, the app's icon: the tray's key,
// in color, on a rounded dark square. Only with KEYBAR_ICON=1.
func TestAppIcon(t *testing.T) {
	if os.Getenv("KEYBAR_ICON") != "1" {
		t.Skip("set KEYBAR_ICON=1 to write resources/icon.png")
	}
	const n = 1024
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	bg := color.NRGBA{R: 0x1c, G: 0x1d, B: 0x21, A: 255}
	r := 220
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := max(r-x, x-(n-1-r), 0), max(r-y, y-(n-1-r), 0)
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	key := keyImage(720, true)
	draw.Draw(img, image.Rect(152, 152, 872, 872), key, image.Point{}, draw.Over)
	_ = os.MkdirAll("resources", 0o755)
	f, err := os.Create("resources/icon.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
