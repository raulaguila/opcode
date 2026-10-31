package theme

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

const CustomThemeName = "custom"
const customPreviewName = "_opcode_custom_preview"
const customThemeLimit = 16 << 10

type CustomPalette struct {
	Base       string        `json:"base"`
	Primary    AdaptiveColor `json:"primary"`
	Text       AdaptiveColor `json:"text"`
	Muted      AdaptiveColor `json:"muted"`
	Background AdaptiveColor `json:"background"`
}
type customTheme struct {
	Theme
	palette CustomPalette
}

func (t *customTheme) Primary() AdaptiveColor         { return t.palette.Primary }
func (t *customTheme) Text() AdaptiveColor            { return t.palette.Text }
func (t *customTheme) TextEmphasized() AdaptiveColor  { return t.palette.Text }
func (t *customTheme) TextMuted() AdaptiveColor       { return t.palette.Muted }
func (t *customTheme) Background() AdaptiveColor      { return t.palette.Background }
func (t *customTheme) MarkdownText() AdaptiveColor    { return t.palette.Text }
func (t *customTheme) MarkdownHeading() AdaptiveColor { return t.palette.Primary }
func CustomThemePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".opcode", "themes", "custom.json"), nil
}
func PaletteFor(name string) (CustomPalette, error) {
	base := GetTheme(name)
	if base == nil {
		return CustomPalette{}, fmt.Errorf("unknown base theme")
	}
	if p, ok := base.(*customTheme); ok {
		return p.palette, nil
	}
	return CustomPalette{Base: name, Primary: base.Primary(), Text: base.Text(), Muted: base.TextMuted(), Background: base.Background()}, nil
}

var literalColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func colorLuminance(s string) float64 {
	n, _ := strconv.ParseUint(s[1:], 16, 24)
	var result float64
	for i, w := range []float64{0.2126, 0.7152, 0.0722} {
		v := float64((n>>uint(16-i*8))&255) / 255
		if v <= 0.04045 {
			v /= 12.92
		} else {
			v = math.Pow((v+0.055)/1.055, 2.4)
		}
		result += w * v
	}
	return result
}
func colorContrast(a, b string) float64 {
	x, y := colorLuminance(a), colorLuminance(b)
	return (max(x, y) + .05) / (min(x, y) + .05)
}
func ValidateCustomPalette(p CustomPalette) error {
	if p.Base == CustomThemeName || p.Base == customPreviewName || GetTheme(p.Base) == nil {
		return fmt.Errorf("invalid base theme")
	}
	for _, pair := range []AdaptiveColor{p.Primary, p.Text, p.Muted, p.Background} {
		if !literalColor.MatchString(pair.Light) || !literalColor.MatchString(pair.Dark) {
			return fmt.Errorf("colors must be literal #RRGGBB for light and dark modes")
		}
	}
	base := GetTheme(p.Base)
	if result0, handled := validateCustomPaletteState(&p, base); handled {
		return result0
	}
	return nil
}
func buildCustom(p CustomPalette) (Theme, error) {
	if err := ValidateCustomPalette(p); err != nil {
		return nil, err
	}
	return &customTheme{Theme: GetTheme(p.Base), palette: p}, nil
}
func PreviewCustomPalette(p CustomPalette) error {
	t, err := buildCustom(p)
	if err != nil {
		return err
	}
	RegisterTheme(customPreviewName, t)
	return PreviewTheme(customPreviewName)
}
func DiscardCustomPreview(restore string) error {
	if err := PreviewTheme(restore); err != nil {
		return err
	}
	globalManager.mu.Lock()
	delete(globalManager.themes, customPreviewName)
	globalManager.mu.Unlock()
	return nil
}
func SaveCustomPalette(p CustomPalette) error {
	t, err := buildCustom(p)
	if err != nil {
		return err
	}
	path, err := CustomThemePath()
	if err != nil {
		return err
	}
	unlock := localstate.Lock(path)
	defer unlock()
	backup, err := snapshotCustomTheme(path)
	if err != nil {
		return err
	}
	if err = localstate.Write(path, p, customThemeLimit); err != nil {
		return err
	}
	previous := ""
	if cfg := config.Get(); cfg != nil {
		previous = cfg.TUI.Theme
	}
	if err = config.UpdateTheme(CustomThemeName); err != nil {
		if cfg := config.Get(); cfg != nil {
			cfg.TUI.Theme = previous
		}
		if rollbackErr := restoreCustomTheme(path, backup); rollbackErr != nil {
			return fmt.Errorf("theme setting could not be saved: %w", errors.Join(err, fmt.Errorf("palette rollback failed: %w", rollbackErr)))
		}
		return fmt.Errorf("theme could not be saved; previous palette restored: %w", err)
	}
	RegisterTheme(CustomThemeName, t)
	return PreviewTheme(CustomThemeName)
}

// Keep the original bytes, including formatting, so a failed settings write
// cannot silently change the palette loaded on the next startup.
func snapshotCustomTheme(path string) ([]byte, error) {
	root, err := localstate.OpenPrivateRoot(filepath.Dir(path), false)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > customThemeLimit {
		return nil, fmt.Errorf("invalid or oversized custom theme backup")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("custom theme changed during backup")
	}
	data, err := io.ReadAll(io.LimitReader(file, customThemeLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > customThemeLimit {
		return nil, fmt.Errorf("custom theme backup exceeds limit")
	}
	return data, nil
}

func restoreCustomTheme(path string, backup []byte) error {
	if backup != nil {
		return localstate.WriteBytes(path, backup)
	}
	root, err := localstate.OpenPrivateRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("custom theme rollback destination must be a regular file")
	}
	return root.Remove(name)
}

func LoadCustomTheme() error {
	path, err := CustomThemePath()
	if err != nil {
		return err
	}
	var p CustomPalette
	if err = localstate.Read(path, customThemeLimit, &p); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("custom theme file cannot be read")
	}
	t, err := buildCustom(p)
	if err != nil {
		return err
	}
	RegisterTheme(CustomThemeName, t)
	return nil
}

func validateCustomPaletteState(p *CustomPalette, base Theme) (extractedResult0 error, handled bool) {
	for _, mode := range []string{"light", "dark"} {
		choose := func(c AdaptiveColor) string {
			if mode == "light" {
				return c.Light
			}
			return c.Dark
		}
		bg := choose(p.Background)
		for label, fg := range map[string]AdaptiveColor{"primary": p.Primary, "text": p.Text, "muted": p.Muted} {
			if colorContrast(choose(fg), bg) < 4.5 {
				return fmt.Errorf("%s/%s contrast must be at least 4.5:1", mode, label), true
			}
			secondary := choose(base.BackgroundSecondary())
			if literalColor.MatchString(secondary) && colorContrast(choose(fg), secondary) < 4.5 {
				return fmt.Errorf("%s/%s needs 4.5:1 on the secondary background", mode, label), true
			}
		}
	}
	return extractedResult0, false
}
