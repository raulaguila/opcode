package theme

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpcodeMarkdownProseStaysNeutralAndCodeReadable(t *testing.T) {
	theme := NewOpcodeTheme()
	require.Equal(t, theme.MarkdownText(), theme.MarkdownHeading())
	require.Equal(t, theme.MarkdownText(), theme.MarkdownStrong())
	code, background := theme.MarkdownCode(), theme.Background()
	require.GreaterOrEqual(t, colorContrast(code.Dark, background.Dark), 4.5)
	require.GreaterOrEqual(t, colorContrast(code.Light, background.Light), 4.5)
}
