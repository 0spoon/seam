package main

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// marioSpritePixelWidth is the number of pixel columns in every Mario
// sprite frame. Every row in every frame must match this width exactly;
// the renderer assumes a rectangular grid.
const marioSpritePixelWidth = 12

// marioSpriteTermHeight is the number of terminal rows the rendered
// sprite occupies. Because we render two pixel rows into one terminal
// row via the upper-half-block glyph, this is half the pixel height
// (14 pixel rows -> 7 terminal rows).
const marioSpriteTermHeight = 7

// marioSpritePalette maps the legend characters used in the sprite
// grids below to the NES-era Super Mario Bros. color palette. Keeping
// the palette close to the original artwork is the whole point of
// showing a pixel Mario instead of a glyph, so these should not drift.
var marioSpritePalette = map[rune]color.Color{
	'R': lipgloss.Color("#E52521"), // cap, shirt, sleeves
	'F': lipgloss.Color("#FCB890"), // face, hands (skin)
	'H': lipgloss.Color("#6A2A00"), // hair, brim, mustache base
	'M': lipgloss.Color("#1B1B1B"), // mustache, eyes
	'B': lipgloss.Color("#5C94FC"), // overalls
	'Y': lipgloss.Color("#FBD000"), // overall buttons
	'S': lipgloss.Color("#3E2010"), // shoes
}

// marioSpriteFrames is the two-frame walk cycle drawn at 12x14 pixels.
// '.' is a transparent pixel (rendered as a blank cell); every other
// rune must exist in marioSpritePalette. The top twelve rows are the
// same across both frames -- only the feet move so the walk reads as
// a subtle step, not a whole-body shake.
//
// The grid is authored as strings so an editor with a fixed-width font
// shows the character art the same way the terminal will.
var marioSpriteFrames = [][]string{
	{
		"...HHHH.....",
		"..HRRRRRR...",
		"..HHFFFMF...",
		".HFHFFFMFF..",
		".HFHHFFFFF..",
		".HHFFFFFF...",
		"...FFFFFF...",
		"..RRRRRRRR..",
		".RRRRBRRBRR.",
		"RRRRBBBBRRRR",
		"FFRRBBBBRRFF",
		"FF.BBBBBB.FF",
		"..BB....BB..",
		"..SS....SS..",
	},
	{
		"...HHHH.....",
		"..HRRRRRR...",
		"..HHFFFMF...",
		".HFHFFFMFF..",
		".HFHHFFFFF..",
		".HHFFFFFF...",
		"...FFFFFF...",
		"..RRRRRRRR..",
		".RRRRBRRBRR.",
		"RRRRBBBBRRRR",
		"FFRRBBBBRRFF",
		"FF.BBBBBB.FF",
		"..BBB..BBB..",
		"...SS..SS...",
	},
}

// marioSpriteFrameCount is the number of distinct walk-cycle frames.
// The Ask screen's animation tick advances through them modulo this
// value so the cycle loops cleanly. It is a var because Go does not
// allow len() over a composite literal in a const declaration.
var marioSpriteFrameCount = len(marioSpriteFrames)

// renderMarioSprite returns the sprite frame at the given index as a
// slice of ANSI-styled terminal lines. Each line is exactly
// marioSpritePixelWidth visible cells wide (transparent pixels become
// plain spaces), and the returned slice has exactly
// marioSpriteTermHeight entries so callers can reserve layout space
// up-front without peeking at the frame contents.
//
// Two pixel rows are packed into one terminal row via the upper-half
// block "▀": its foreground is the top pixel's color, its background
// is the bottom pixel's color. When either half is transparent we
// fall back to a half-block that only carries the opaque side (or a
// plain space when both halves are transparent) so the terminal's
// own background shows through cleanly.
func renderMarioSprite(frame int) []string {
	rows := marioSpriteFrames[((frame%marioSpriteFrameCount)+marioSpriteFrameCount)%marioSpriteFrameCount]
	out := make([]string, 0, marioSpriteTermHeight)
	for y := 0; y < len(rows); y += 2 {
		topRow := []rune(rows[y])
		var botRow []rune
		if y+1 < len(rows) {
			botRow = []rune(rows[y+1])
		} else {
			botRow = []rune(strings.Repeat(".", marioSpritePixelWidth))
		}
		var b strings.Builder
		for x := 0; x < marioSpritePixelWidth; x++ {
			tc, tOK := marioSpritePalette[topRow[x]]
			bc, bOK := marioSpritePalette[botRow[x]]
			switch {
			case !tOK && !bOK:
				b.WriteString(" ")
			case tOK && bOK:
				b.WriteString(lipgloss.NewStyle().
					Foreground(tc).
					Background(bc).
					Render("▀"))
			case tOK:
				b.WriteString(lipgloss.NewStyle().
					Foreground(tc).
					Render("▀"))
			case bOK:
				b.WriteString(lipgloss.NewStyle().
					Foreground(bc).
					Render("▄"))
			}
		}
		out = append(out, b.String())
	}
	return out
}
