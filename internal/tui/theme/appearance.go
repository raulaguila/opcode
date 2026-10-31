package theme

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// AdaptiveColor keeps the persisted light/dark string pairs independent of
// renderer versions. Terminal capabilities are supplied by the application.
type AdaptiveColor struct {
	Light string
	Dark  string
}

var appearanceProfile atomic.Uint32
var appearanceDark atomic.Bool

func init() {
	SetColorProfile(colorprofile.Detect(os.Stdout, os.Environ()))
	dark := true
	if values := strings.Split(os.Getenv("COLORFGBG"), ";"); len(values) > 1 {
		if background, err := strconv.Atoi(values[len(values)-1]); err == nil {
			dark = background < 7 || background == 8
		}
	}
	SetHasDarkBackground(dark)
}

func (c AdaptiveColor) RGBA() (uint32, uint32, uint32, uint32) {
	value := c.Light
	if HasDarkBackground() {
		value = c.Dark
	}
	if color := lipgloss.Color(value); color != nil {
		return color.RGBA()
	}
	return 0, 0, 0, 0
}

func ColorProfile() colorprofile.Profile           { return colorprofile.Profile(appearanceProfile.Load()) }
func SetColorProfile(profile colorprofile.Profile) { appearanceProfile.Store(uint32(profile)) }
func HasDarkBackground() bool                      { return appearanceDark.Load() }
func SetHasDarkBackground(dark bool)               { appearanceDark.Store(dark) }
