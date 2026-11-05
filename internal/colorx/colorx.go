// Package colorx generates subcategory colors as shades of a category's base hue (CAT-5).
package colorx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Shade returns the i-th of n shades of the given base hex color (#rrggbb),
// varying only the HSL lightness around the base lightness.
func Shade(baseHex string, i, n int) (string, error) {
	r, g, b, err := hexToRGB(baseHex)
	if err != nil {
		return "", err
	}
	h, s, l := rgbToHSL(r, g, b)
	step := 0.09
	offset := (float64(i) - float64(n-1)/2.0) * step
	l = clamp(l+offset, 0.22, 0.82)
	r, g, b = hslToRGB(h, s, l)
	return rgbToHex(r, g, b), nil
}

func clamp(v, min, max float64) float64 {
	return math.Max(min, math.Min(max, v))
}

func hexToRGB(hex string) (int, int, int, error) {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q", hex)
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", hex, err)
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), nil
}

func rgbToHex(r, g, b int) string {
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func rgbToHSL(r, g, b int) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	l = (max + min) / 2
	d := max - min
	if d == 0 {
		return 0, 0, l
	}
	s = d / (1 - math.Abs(2*l-1))
	switch max {
	case rf:
		h = 60 * math.Mod((gf-bf)/d, 6)
	case gf:
		h = 60 * ((bf-rf)/d + 2)
	default:
		h = 60 * ((rf-gf)/d + 4)
	}
	if h < 0 {
		h += 360
	}
	return h, s, l
}

func hslToRGB(h, s, l float64) (int, int, int) {
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var rf, gf, bf float64
	switch {
	case hp < 1:
		rf, gf, bf = c, x, 0
	case hp < 2:
		rf, gf, bf = x, c, 0
	case hp < 3:
		rf, gf, bf = 0, c, x
	case hp < 4:
		rf, gf, bf = 0, x, c
	case hp < 5:
		rf, gf, bf = x, 0, c
	default:
		rf, gf, bf = c, 0, x
	}
	m := l - c/2
	return int(math.Round((rf + m) * 255)), int(math.Round((gf + m) * 255)), int(math.Round((bf + m) * 255))
}
