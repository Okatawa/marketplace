package demo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// Photo creates an original geometric fixture; it needs no external assets.
func Photo(kind int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	colors := []color.RGBA{{203, 213, 177, 255}, {226, 199, 166, 255}, {177, 201, 214, 255}, {222, 189, 188, 255}}
	c := colors[kind%len(colors)]
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			col := c
			if x > 160 && x < 480 && y > 100 && y < 380 {
				col = color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255}
			}
			if (x-320)*(x-320)+(y-225)*(y-225) < 70*70 {
				col = color.RGBA{245, 243, 227, 255}
			}
			img.SetRGBA(x, y, col)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}
