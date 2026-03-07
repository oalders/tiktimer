package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
)

//go:embed digits/*.gif
var digitFiles embed.FS

var digitImages [10]image.Image
var colonImage image.Image
var dotImage image.Image

func init() {
	for i := 0; i < 10; i++ {
		data, err := digitFiles.ReadFile(fmt.Sprintf("digits/%d.gif", i))
		if err != nil {
			panic(err)
		}
		img, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			panic(err)
		}
		digitImages[i] = img
	}

	// Create colon separator: black background with two white dots
	h := digitImages[0].Bounds().Dy() // 20
	w := 7
	colon := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(colon, colon.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	white := color.White
	// Upper dot
	for y := 6; y <= 8; y++ {
		for x := 2; x <= 4; x++ {
			colon.Set(x, y, white)
		}
	}
	// Lower dot
	for y := 12; y <= 14; y++ {
		for x := 2; x <= 4; x++ {
			colon.Set(x, y, white)
		}
	}
	colonImage = colon

	// Create dot separator: black background with one white dot at bottom
	dotImg := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dotImg, dotImg.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	for y := 16; y <= 18; y++ {
		for x := 2; x <= 4; x++ {
			dotImg.Set(x, y, white)
		}
	}
	dotImage = dotImg
}

func renderOdometer(timeStr string) string {
	// Calculate total width
	totalWidth := 0
	var images []image.Image
	for _, ch := range timeStr {
		switch {
		case ch >= '0' && ch <= '9':
			img := digitImages[ch-'0']
			images = append(images, img)
			totalWidth += img.Bounds().Dx()
		case ch == ':':
			images = append(images, colonImage)
			totalWidth += colonImage.Bounds().Dx()
		case ch == '.':
			images = append(images, dotImage)
			totalWidth += dotImage.Bounds().Dx()
		}
	}

	if len(images) == 0 {
		return ""
	}

	h := digitImages[0].Bounds().Dy()
	composite := image.NewRGBA(image.Rect(0, 0, totalWidth, h))
	x := 0
	for _, img := range images {
		draw.Draw(composite, image.Rect(x, 0, x+img.Bounds().Dx(), h), img, image.Point{}, draw.Src)
		x += img.Bounds().Dx()
	}

	var buf bytes.Buffer
	png.Encode(&buf, composite)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
