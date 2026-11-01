package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"cashstratumapi/cmd/cashtop/internal/termcap"
)

// Theme holds the styles of one colour scheme.
type Theme struct {
	Name       string
	Background color.Color // nil leaves the terminal background alone

	Text, Dim, Accent, Title     lipgloss.Style
	Good, Warn, Bad, Gold        lipgloss.Style
	Alert                        lipgloss.Style // full-width warning banner
	TabActive, TabIdle, Selected lipgloss.Style

	// Palette is indexed by FeedRow.Colour.
	Palette []lipgloss.Style
}

const (
	cashGreen = "#0AC18E"
	nearBlack = "#0B0F0E"
)

// Cashgreen is the default theme: BCH green on near-black.
func Cashgreen() Theme {
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	green := fg(cashGreen)
	t := Theme{
		Name:       "cashgreen",
		Background: lipgloss.Color(nearBlack),
		Text:       fg("#D7E4DF"),
		Dim:        fg("#5E7069"),
		Accent:     green,
		Title:      green.Bold(true),
		Good:       green,
		Warn:       fg("#E5B94E"),
		Bad:        fg("#E5534B"),
		Gold:       fg("#F2C94C").Bold(true),
		Alert:      lipgloss.NewStyle().Foreground(lipgloss.Color(nearBlack)).Background(lipgloss.Color("#E5534B")).Bold(true),
		TabActive:  lipgloss.NewStyle().Foreground(lipgloss.Color(nearBlack)).Background(lipgloss.Color(cashGreen)).Bold(true),
		TabIdle:    fg("#8FA39B"),
		Selected:   lipgloss.NewStyle().Background(lipgloss.Color("#16302A")),
	}
	t.Palette = []lipgloss.Style{t.Text, t.Accent, t.Warn, t.Bad, t.Gold, t.Dim, fg("#6CB6FF"), fg("#C39BD3")}
	return t
}

// Mono uses no colour at all; selection and the active tab use reverse video.
func Mono() Theme {
	p := lipgloss.NewStyle()
	return Theme{
		Name: "mono", Text: p, Dim: p, Accent: p, Title: p.Bold(true),
		Good: p, Warn: p, Bad: p, Gold: p.Bold(true), Alert: p.Bold(true).Reverse(true),
		TabActive: p.Reverse(true), TabIdle: p, Selected: p.Reverse(true),
		Palette: []lipgloss.Style{p},
	}
}

// ThemeFor picks mono when colour is disabled, cashgreen otherwise.
func ThemeFor(c termcap.Caps) Theme {
	if c.NoColor {
		return Mono()
	}
	return Cashgreen()
}

func (t Theme) palette(i int) lipgloss.Style {
	if len(t.Palette) == 0 {
		return t.Text
	}
	if i < 0 {
		i = -i
	}
	return t.Palette[i%len(t.Palette)]
}

// symbols are the non-ASCII characters the views use, with ASCII stand-ins.
type symbols struct {
	Sep, Bullet, Live, Stale, Idle, Off, Cursor, Ellipsis, Up, Down string
	Beat                                                            []string
	MeterFull, MeterEmpty, MeterTick, MeterDue                      string
	MeterRug                                                        string
	MeterRamp                                                       [3]string
}

var (
	utf8Symbols = symbols{
		Sep: "·", Bullet: "•", Live: "●", Stale: "◐", Idle: "◌", Off: "○", Cursor: "▸", Ellipsis: "…",
		Up: "▲", Down: "▼", Beat: []string{"●", "◉", "○", "◉"},
		MeterFull: "█", MeterEmpty: "░", MeterTick: "┃", MeterDue: "╎",
		MeterRug: "·", MeterRamp: [3]string{"┃", "╋", "█"},
	}
	asciiSymbols = symbols{
		Sep: "|", Bullet: "*", Live: "+", Stale: "~", Idle: ".", Off: "-", Cursor: ">", Ellipsis: "~",
		Up: "^", Down: "v", Beat: []string{"*"},
		MeterFull: "#", MeterEmpty: ".", MeterTick: "|", MeterDue: ":",
		MeterRug: ".", MeterRamp: [3]string{"|", "!", "#"},
	}
)

func symbolsFor(c termcap.Caps) symbols {
	if c.Glyphs == termcap.ASCII {
		return asciiSymbols
	}
	return utf8Symbols
}
