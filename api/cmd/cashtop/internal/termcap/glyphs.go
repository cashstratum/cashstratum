package termcap

// Glyph tables for drawing; every table has an ASCII counterpart.

var (
	sparkUTF8  = []rune("▁▂▃▄▅▆▇█")
	sparkASCII = []rune("_.-=#")
)

// SparkRamp returns the sparkline ramp from lowest to highest.
func (c Caps) SparkRamp() []rune {
	if c.Glyphs == ASCII {
		return sparkASCII
	}
	return sparkUTF8
}

// SparkLevel maps frac in [0,1] to a ramp rune.
func (c Caps) SparkLevel(frac float64) rune {
	r := c.SparkRamp()
	if frac != frac || frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	return r[int(frac*float64(len(r)-1)+0.5)]
}

// BrailleOK reports whether braille cells may be drawn.
func (c Caps) BrailleOK() bool { return c.Glyphs == UTF8 }

// BrailleFallback returns the replacement for a braille cell: a block when
// any dot is set and UTF-8 is available, '.' / ' ' in ASCII.
func (c Caps) BrailleFallback(r rune) rune {
	set := r > 0x2800 && r <= 0x28FF
	switch {
	case c.Glyphs == UTF8 && set:
		return '█'
	case c.Glyphs == UTF8:
		return ' '
	case set:
		return '.'
	}
	return ' '
}

// Border holds the characters of a box.
type Border struct {
	TL, TR, BL, BR, H, V string
}

// Box returns the box-drawing set for the glyph mode.
func (c Caps) Box() Border {
	if c.Glyphs == ASCII {
		return Border{TL: "+", TR: "+", BL: "+", BR: "+", H: "-", V: "|"}
	}
	return Border{TL: "┌", TR: "┐", BL: "└", BR: "┘", H: "─", V: "│"}
}
