package logo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

type Style int

const (
	App Style = iota
	TrayOnDark
	TrayOnLight
)

// Фирменные цвета
var (
	Espresso = color.NRGBA{R: 0x23, G: 0x19, B: 0x0e, A: 0xff}
	Sage     = color.NRGBA{R: 0x59, G: 0x81, B: 0x75, A: 0xff}
	Cream    = color.NRGBA{R: 0xf3, G: 0xec, B: 0xdd, A: 0xff}

	trayLight = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	trayDark  = color.NRGBA{R: 0x1f, G: 0x1a, B: 0x14, A: 0xff}
)

const (
	boxCX, boxCY = 0.44, 0.57 // центр чекбокса
	boxHalf      = 0.33       // половина стороны
	boxRadius    = 0.11       // скругление углов

	ax, ay = 0.31, 0.57
	bx, by = 0.45, 0.71
	cx, cy = 0.91, 0.15

	gap = 0.065 // зазор между галочкой и контуром в месте выхода
)

func Render(size int, style Style) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	if size <= 0 {
		return img
	}
	small := size <= 24
	stroke, check, scale := 0.085, 0.115, 0.76
	if small {
		stroke, check, scale = 0.105, 0.14, 0.92
	}
	if style != App {
		scale = 1
	}
	m := mark{stroke: stroke, check: check}
	centerX, centerY, fit := m.layout()
	scale *= fit

	const sub = 5
	for py := range size {
		for px := range size {
			var r, g, b, a float64
			for sy := range sub {
				for sx := range sub {
					x := (float64(px) + (float64(sx)+0.5)/sub) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/sub) / float64(size)
					// Точка знака, которая попадает в эту точку холста.
					gx, gy := (x-0.5)/scale+centerX, (y-0.5)/scale+centerY
					if c, ok := m.sample(x, y, gx, gy, style); ok {
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
						a++
					}
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(r / a), G: uint8(g / a), B: uint8(b / a),
				A: uint8(math.Round(255 * a / (sub * sub))),
			})
		}
	}
	return img
}

func PNG(size int, style Style) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Render(size, style)); err != nil {
		return nil, fmt.Errorf("encode logo: %w", err)
	}
	return buf.Bytes(), nil
}

type mark struct {
	stroke, check float64
}

func (m mark) ink(gx, gy float64) (inCheck, inBox bool) {
	toCheck := math.Min(segment(gx, gy, ax, ay, bx, by), segment(gx, gy, bx, by, cx, cy))
	inCheck = toCheck <= m.check/2
	toBox := math.Abs(roundRect(gx, gy, boxCX, boxCY, boxHalf, boxRadius))
	inBox = toBox <= m.stroke/2 && toCheck > m.check/2+gap
	return inCheck, inBox
}

func (m mark) layout() (centerX, centerY, fit float64) {
	const grid = 240
	const margin = 0.05

	var sumX, sumY, n float64
	minX, minY, maxX, maxY := 1.0, 1.0, 0.0, 0.0
	for iy := range grid {
		for ix := range grid {
			x, y := (float64(ix)+0.5)/grid, (float64(iy)+0.5)/grid
			if inCheck, inBox := m.ink(x, y); !inCheck && !inBox {
				continue
			}
			sumX, sumY, n = sumX+x, sumY+y, n+1
			minX, minY = math.Min(minX, x), math.Min(minY, y)
			maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
		}
	}
	centerX, centerY = sumX/n, sumY/n

	reach := math.Max(math.Max(centerX-minX, maxX-centerX), math.Max(centerY-minY, maxY-centerY))
	fit = math.Min(1, (0.5-margin)/reach)
	return centerX, centerY, fit
}

func (m mark) sample(x, y, gx, gy float64, style Style) (color.NRGBA, bool) {
	inCheck, inBox := m.ink(gx, gy)

	switch style {
	case App:
		switch {
		case inCheck:
			return Cream, true
		case inBox:
			return Sage, true
		case roundRect(x, y, 0.5, 0.5, 0.48, 0.22) <= 0:
			return Espresso, true
		}
	case TrayOnDark:
		if inCheck || inBox {
			return trayLight, true
		}
	case TrayOnLight:
		if inCheck || inBox {
			return trayDark, true
		}
	}
	return color.NRGBA{}, false
}

func roundRect(x, y, cx, cy, half, radius float64) float64 {
	qx := math.Abs(x-cx) - (half - radius)
	qy := math.Abs(y-cy) - (half - radius)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
}

func segment(x, y, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	t := ((x-x1)*dx + (y-y1)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(x-(x1+t*dx), y-(y1+t*dy))
}
