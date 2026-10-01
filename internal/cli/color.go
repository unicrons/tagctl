package cli

import (
	"io"

	"github.com/unicrons/tagctl/internal/log"
)

// palette holds the ANSI codes used on one stream. Every field is empty when
// the stream gets no colour, so call sites format the same way either way.
type palette struct {
	reset, red, green, yellow, cyan, bold, dim string
}

var ansiPalette = palette{
	reset:  "\033[0m",
	red:    "\033[31m",
	green:  "\033[32m",
	yellow: "\033[33m",
	cyan:   "\033[36m",
	bold:   "\033[1m",
	dim:    "\033[2m",
}

// paletteFor returns the colours to use on w, as log.UseColor decides.
func paletteFor(w io.Writer) palette {
	if log.UseColor(w) {
		return ansiPalette
	}
	return palette{}
}
