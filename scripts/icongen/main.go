// Command icongen renders the kubeon app icon (the titlebar mark: a helm
// wheel on an accent tile) into the PNG/ICO/ICNS files used by the
// packages: go run ./scripts/icongen -out packaging/icons
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/vector"
)

var (
	top    = color.NRGBA{0x4f, 0xa3, 0xe0, 0xff} // accent, lighter
	bottom = color.NRGBA{0x2b, 0x6f, 0xad, 0xff} // accent, darker
	white  = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

const k = 0.5523 // circle bezier factor

func circle(z *vector.Rasterizer, cx, cy, r float32, ccw bool) {
	if !ccw {
		z.MoveTo(cx+r, cy)
		z.CubeTo(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
		z.CubeTo(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
		z.CubeTo(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
		z.CubeTo(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
	} else {
		z.MoveTo(cx+r, cy)
		z.CubeTo(cx+r, cy-r*k, cx+r*k, cy-r, cx, cy-r)
		z.CubeTo(cx-r*k, cy-r, cx-r, cy-r*k, cx-r, cy)
		z.CubeTo(cx-r, cy+r*k, cx-r*k, cy+r, cx, cy+r)
		z.CubeTo(cx+r*k, cy+r, cx+r, cy+r*k, cx+r, cy)
	}
	z.ClosePath()
}

func roundRect(z *vector.Rasterizer, x0, y0, x1, y1, r float32) {
	z.MoveTo(x0+r, y0)
	z.LineTo(x1-r, y0)
	z.CubeTo(x1-r+r*k, y0, x1, y0+r-r*k, x1, y0+r)
	z.LineTo(x1, y1-r)
	z.CubeTo(x1, y1-r+r*k, x1-r+r*k, y1, x1-r, y1)
	z.LineTo(x0+r, y1)
	z.CubeTo(x0+r-r*k, y1, x0, y1-r+r*k, x0, y1-r)
	z.LineTo(x0, y0+r)
	z.CubeTo(x0, y0+r-r*k, x0+r-r*k, y0, x0+r, y0)
	z.ClosePath()
}

// bar fills a line segment of width w with round caps.
func bar(z *vector.Rasterizer, x0, y0, x1, y1, w float32) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Hypot(float64(dx), float64(dy)))
	nx, ny := -dy/l*w/2, dx/l*w/2
	z.MoveTo(x0+nx, y0+ny)
	z.LineTo(x1+nx, y1+ny)
	z.LineTo(x1-nx, y1-ny)
	z.LineTo(x0-nx, y0-ny)
	z.ClosePath()
}

func render(size int) *image.NRGBA {
	s := float32(size)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	// macOS-style safe area on large sizes, almost full bleed on tiny ones
	m := s * 100 / 1024
	if size <= 32 {
		m = s / 32
	}
	tile := s - 2*m

	grad := image.NewNRGBA(img.Rect)
	for y := 0; y < size; y++ {
		t := float32(y) / s
		c := color.NRGBA{
			uint8(float32(top.R)*(1-t) + float32(bottom.R)*t),
			uint8(float32(top.G)*(1-t) + float32(bottom.G)*t),
			uint8(float32(top.B)*(1-t) + float32(bottom.B)*t), 0xff,
		}
		for x := 0; x < size; x++ {
			grad.SetNRGBA(x, y, c)
		}
	}
	paint := func(src image.Image, build func(z *vector.Rasterizer)) {
		z := vector.NewRasterizer(size, size)
		z.DrawOp = draw.Over
		build(z)
		z.Draw(img, img.Rect, src, image.Point{})
	}
	paint(grad, func(z *vector.Rasterizer) { roundRect(z, m, m, s-m, s-m, tile*0.225) })

	fg := image.NewUniform(white)
	c := s / 2
	r := tile * 0.28
	sw := max(tile*0.075, 1.6)
	paint(fg, func(z *vector.Rasterizer) { circle(z, c, c, r+sw/2, false); circle(z, c, c, r-sw/2, true) })
	R := tile * 0.40
	for _, a := range []float64{0, 45, 90, 135} {
		dx, dy := float32(math.Cos(a*math.Pi/180))*R, float32(math.Sin(a*math.Pi/180))*R
		paint(fg, func(z *vector.Rasterizer) { bar(z, c-dx, c-dy, c+dx, c+dy, sw) })
		if size >= 48 { // handle knobs on the spoke ends
			paint(fg, func(z *vector.Rasterizer) { circle(z, c-dx, c-dy, sw*0.9, false) })
			paint(fg, func(z *vector.Rasterizer) { circle(z, c+dx, c+dy, sw*0.9, false) })
		}
	}
	paint(fg, func(z *vector.Rasterizer) { circle(z, c, c, sw*1.1, false) })
	return img
}

func encode(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

// writeICO stores PNG-compressed entries (Windows Vista+).
func writeICO(path string, sizes []int, pngs map[int][]byte) error {
	var b bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&b, le, []uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for _, s := range sizes {
		d := uint8(s)
		if s >= 256 {
			d = 0
		}
		_ = binary.Write(&b, le, []uint8{d, d, 0, 0})
		_ = binary.Write(&b, le, []uint16{1, 32})
		_ = binary.Write(&b, le, []uint32{uint32(len(pngs[s])), uint32(off)})
		off += len(pngs[s])
	}
	for _, s := range sizes {
		b.Write(pngs[s])
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// writeICNS stores PNG entries (macOS 10.7+).
func writeICNS(path string, pngs map[int][]byte) error {
	types := []struct {
		t string
		s int
	}{{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024}, {"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512}}
	var body bytes.Buffer
	for _, e := range types {
		body.WriteString(e.t)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(pngs[e.s])))
		body.Write(pngs[e.s])
	}
	var b bytes.Buffer
	b.WriteString("icns")
	_ = binary.Write(&b, binary.BigEndian, uint32(8+body.Len()))
	b.Write(body.Bytes())
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func main() {
	out := flag.String("out", "packaging/icons", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256, 512, 1024}
	pngs := map[int][]byte{}
	for _, s := range sizes {
		pngs[s] = encode(render(s))
		if err := os.WriteFile(filepath.Join(*out, fmt.Sprintf("kubeon-%d.png", s)), pngs[s], 0o644); err != nil {
			log.Fatal(err)
		}
	}
	if err := writeICO(filepath.Join(*out, "kubeon.ico"), []int{16, 24, 32, 48, 64, 128, 256}, pngs); err != nil {
		log.Fatal(err)
	}
	if err := writeICNS(filepath.Join(*out, "kubeon.icns"), pngs); err != nil {
		log.Fatal(err)
	}
}
