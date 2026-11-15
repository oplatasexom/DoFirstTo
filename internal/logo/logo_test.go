package logo

import (
	"math"
	"testing"
)

func inkCenter(size int, style Style) (cx, cy float64, left, top, right, bottom int) {
	img := Render(size, style)
	var sumX, sumY, total float64
	left, top, right, bottom = size, size, size, size
	for y := range size {
		for x := range size {
			c := img.NRGBAAt(x, y)
			ink := c.A > 0
			if style == App {
				ink = c.A > 0 && c != Espresso
			}
			if !ink {
				continue
			}
			w := float64(c.A) / 255
			sumX, sumY, total = sumX+w*(float64(x)+0.5), sumY+w*(float64(y)+0.5), total+w
			left, top = min(left, x), min(top, y)
			right, bottom = min(right, size-1-x), min(bottom, size-1-y)
		}
	}
	return sumX / total / float64(size), sumY / total / float64(size), left, top, right, bottom
}

func TestMarkIsOpticallyCentred(t *testing.T) {
	tests := []struct {
		name  string
		size  int
		style Style
	}{
		{"app icon", 256, App},
		{"small app icon", 16, App},
		{"tray on dark", 32, TrayOnDark},
		{"tray on light", 16, TrayOnLight},
		{"large tray", 256, TrayOnDark},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cx, cy, left, top, right, bottom := inkCenter(tt.size, tt.style)
			tolerance := math.Max(0.012, 0.6/float64(tt.size))
			if math.Abs(cx-0.5) > tolerance || math.Abs(cy-0.5) > tolerance {
				t.Errorf("ink centre = (%.3f, %.3f), want (0.5, 0.5) within %.3f", cx, cy, tolerance)
			}
			if tt.size >= 32 && (left < 1 || top < 1 || right < 1 || bottom < 1) {
				t.Errorf("mark touches the edge: margins left %d, top %d, right %d, bottom %d", left, top, right, bottom)
			}
		})
	}
}

func TestLayout(t *testing.T) {
	for _, m := range []mark{{stroke: 0.085, check: 0.115}, {stroke: 0.105, check: 0.14}} {
		cx, cy, fit := m.layout()
		if cx <= boxCX || cy >= boxCY {
			t.Errorf("layout centre = (%.3f, %.3f), want it right of %.2f and above %.2f", cx, cy, boxCX, boxCY)
		}
		if fit <= 0.8 || fit > 1 {
			t.Errorf("fit = %.3f, want a mild shrink in (0.8, 1]", fit)
		}
		t.Logf("stroke %.3f: centre (%.4f, %.4f), fit %.4f", m.stroke, cx, cy, fit)
	}
}

func TestRenderSizes(t *testing.T) {
	if got := Render(0, App).Bounds().Dx(); got != 0 {
		t.Errorf("Render(0) width = %d, want 0", got)
	}
	data, err := PNG(32, TrayOnDark)
	if err != nil || len(data) < 100 {
		t.Errorf("PNG = %d bytes, %v", len(data), err)
	}
}
