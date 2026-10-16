package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"time"
)

// Today-bar images are drawn at 2x and tagged 144 dpi (see withRetinaDPI), so
// AppKit shows them at half their pixel size: the bar is 260x16pt, the
// swatch 16x16pt.
const (
	barW, barH     = 520, 32
	barTop, barBot = 9, 23 // pill rows, 14px tall
	barLeft        = 2
	barRight       = barW - 2
	swatchSize     = 32
	swatchDiam     = 16
)

// projectPalette holds saturated mid-tone hues that read on both the light and
// the dark menu background. Colors cycle when there are more projects.
var projectPalette = []color.NRGBA{
	{0x3B, 0x82, 0xF6, 0xFF}, // blue
	{0xF5, 0x9E, 0x0B, 0xFF}, // orange
	{0x22, 0xC5, 0x5E, 0xFF}, // green
	{0xA8, 0x55, 0xF7, 0xFF}, // purple
	{0xEC, 0x48, 0x99, 0xFF}, // pink
	{0x14, 0xB8, 0xA6, 0xFF}, // teal
}

var (
	trackColor = color.NRGBA{0x80, 0x80, 0x80, 0x48}
	tickDark   = color.NRGBA{0x30, 0x30, 0x30, 0xFF}
	tickLight  = color.NRGBA{0xFF, 0xFF, 0xFF, 0xE0}
)

func paletteColor(i int) color.NRGBA {
	return projectPalette[i%len(projectPalette)]
}

// unloggedShade is a project's color at reduced opacity, marking time still on
// a timer as distinct from time already logged.
func unloggedShade(c color.NRGBA) color.NRGBA {
	c.A = 0x70
	return c
}

// pillCoverage reports how much of pixel (px, py) lies inside the rounded bar,
// from 4x4 supersampling, so the ends are anti-aliased.
func pillCoverage(px, py int) float64 {
	const n = 4
	r := float64(barBot-barTop) / 2
	cy := float64(barTop) + r
	inside := 0
	for sy := 0; sy < n; sy++ {
		for sx := 0; sx < n; sx++ {
			x := float64(px) + (float64(sx)+0.5)/n
			y := float64(py) + (float64(sy)+0.5)/n
			if y < float64(barTop) || y > float64(barBot) {
				continue
			}
			cx := x
			if lo := float64(barLeft) + r; x < lo {
				cx = lo
			} else if hi := float64(barRight) - r; x > hi {
				cx = hi
			}
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy <= r*r {
				inside++
			}
		}
	}
	return float64(inside) / (n * n)
}

// renderDayBar draws the day's stacked bar: per project, a solid segment for
// logged time then a translucent one for unlogged timer time, over a grey
// track scaled to max(target, total). Past the target, a tick marks where the
// target falls.
func renderDayBar(projects []projectTotal, target time.Duration) string {
	var total time.Duration
	for _, p := range projects {
		total += p.Total()
	}
	scale := target
	if total > scale {
		scale = total
	}

	// Color each pixel column by the segment it falls in.
	span := barRight - barLeft
	cols := make([]color.NRGBA, span)
	for i := range cols {
		cols[i] = trackColor
	}
	if scale > 0 {
		var acc time.Duration
		paint := func(d time.Duration, c color.NRGBA) {
			from := int(float64(acc) / float64(scale) * float64(span))
			acc += d
			to := int(float64(acc) / float64(scale) * float64(span))
			for x := from; x < to && x < span; x++ {
				cols[x] = c
			}
		}
		for _, p := range projects {
			c := paletteColor(p.Color)
			paint(p.Logged, c)
			paint(p.Unlogged, unloggedShade(c))
		}
	}

	img := image.NewNRGBA(image.Rect(0, 0, barW, barH))
	for y := barTop; y < barBot; y++ {
		for x := barLeft; x < barRight; x++ {
			cov := pillCoverage(x, y)
			if cov == 0 {
				continue
			}
			c := cols[x-barLeft]
			c.A = uint8(float64(c.A) * cov)
			img.SetNRGBA(x, y, c)
		}
	}

	if total > target && scale > 0 {
		tx := barLeft + int(float64(target)/float64(scale)*float64(span))
		for y := barTop - 5; y <= barBot+5; y++ {
			img.SetNRGBA(tx-2, y, tickLight)
			img.SetNRGBA(tx-1, y, tickDark)
			img.SetNRGBA(tx, y, tickDark)
			img.SetNRGBA(tx+1, y, tickLight)
		}
	}
	return pngDataURI(img)
}

// renderSwatch draws the legend dot for palette index i.
func renderSwatch(i int) string {
	c := paletteColor(i)
	img := image.NewNRGBA(image.Rect(0, 0, swatchSize, swatchSize))
	r := float64(swatchDiam) / 2
	mid := float64(swatchSize) / 2
	const n = 4
	for py := 0; py < swatchSize; py++ {
		for px := 0; px < swatchSize; px++ {
			inside := 0
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					dx := float64(px) + (float64(sx)+0.5)/n - mid
					dy := float64(py) + (float64(sy)+0.5)/n - mid
					if dx*dx+dy*dy <= r*r {
						inside++
					}
				}
			}
			if inside > 0 {
				cc := c
				cc.A = uint8(float64(c.A) * float64(inside) / (n * n))
				img.SetNRGBA(px, py, cc)
			}
		}
	}
	return pngDataURI(img)
}

func pngDataURI(img image.Image) string {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(withRetinaDPI(buf.Bytes()))
}

// withRetinaDPI inserts a pHYs chunk declaring 144 dpi right after IHDR. Go's
// encoder writes no DPI, so AppKit would size the image at one point per
// pixel and menuet would resample anything over 16pt tall; at 144 dpi a
// 32px-tall image is 16pt and its 2x pixels are shown as-is.
func withRetinaDPI(p []byte) []byte {
	const ihdrEnd = 8 + 4 + 4 + 13 + 4 // signature + IHDR length, type, data, CRC
	if len(p) < ihdrEnd {
		return p
	}
	const pxPerMeter = 5669 // 144 dpi
	chunk := make([]byte, 0, 21)
	chunk = binary.BigEndian.AppendUint32(chunk, 9)
	chunk = append(chunk, "pHYs"...)
	chunk = binary.BigEndian.AppendUint32(chunk, pxPerMeter)
	chunk = binary.BigEndian.AppendUint32(chunk, pxPerMeter)
	chunk = append(chunk, 1) // unit: meter
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	out := make([]byte, 0, len(p)+len(chunk))
	out = append(out, p[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, p[ihdrEnd:]...)
}
