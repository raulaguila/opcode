package theme

import (
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// Ground is the application's background, the one place that defines it.
var Ground color.Color = lipgloss.Color("#16161e")

var groundInUse atomic.Bool

// UseGround puts shared rendering on Ground; the previous interface keeps its
// theme's background.
func UseGround() { groundInUse.Store(true) }

// Surface is the background text sits on.
func Surface() color.Color {
	if groundInUse.Load() {
		return Ground
	}
	return CurrentTheme().Background()
}
