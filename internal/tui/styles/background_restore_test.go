package styles

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestRestoreBackgroundMatchesForcedPixelsAndAttributes(t *testing.T) {
	bg := color.RGBA{R: 22, G: 33, B: 44, A: 255}
	const prefix = "\x1b[48;2;22;33;44m"
	for _, input := range []string{
		"plain ação 漢字 👩‍💻  \nnext  ",
		"\x1b[1;3;4;38;2;42;49;100mstyled\x1b[m plain  ",
		"\x1b[38;2;0;49;100;58;5;49mcolor\x1b[39;59mplain",
		"\x1b[38:2::42:49:100;58:2::42:49:100;4:3mcolor\x1b[m  ",
		"\x1b[48;2;1;2;3mwrong background\x1b[49mroot  ",
		"\x1b[41mred\x1b[100mbright\x1b[mroot",
		"\x1b[;38;5;107mreset\x1b[1;;31mreset\x1b[1;mend  ",
		"\x1b[7mselected\x1b[27mnormal\x1b[0m done  ",
		"\x1b[48:5:42mcolor\x1b[48:2::1:2:3mcolor\x1b[mroot  ",
	} {
		want := prefix + ForceReplaceBackgroundWithLipgloss(input, bg)
		got := restoreBackground(input, bg)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.Ascii, colorprofile.NoTTY} {
			expected := uv.NewStyledString(backgroundProfile(want, profile)).Lines(ansi.GraphemeWidth)
			actual := uv.NewStyledString(backgroundProfile(got, profile)).Lines(ansi.GraphemeWidth)
			require.Equal(t, expected, actual, "input %q, profile %v", input, profile)
		}
	}
}

func TestRestoreBackgroundLeavesForegroundOnlySGRVerbatim(t *testing.T) {
	bg := color.RGBA{R: 22, G: 33, B: 44, A: 255}
	for _, params := range []string{"1;3;4", "38;2;0;49;100", "58;5;49", "38:2::0:49:100;58:5:42", "39;59;22;23;24;27"} {
		input := "\x1b[" + params + "mtext  "
		require.Equal(t, "\x1b[48;2;22;33;44m"+input, restoreBackground(input, bg))
	}
	require.Empty(t, restoreBackground("", bg))
}

func backgroundProfile(input string, profile colorprofile.Profile) string {
	var out strings.Builder
	writer := colorprofile.Writer{Forward: &out, Profile: profile}
	_, _ = writer.WriteString(input)
	return out.String()
}
