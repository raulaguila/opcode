package tools

import (
	"encoding/json"
	"testing"

	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestBashReportsOutputDroppedByProcessBuffers(t *testing.T) {
	testUnisolated(t)
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	response, err := NewBashTool(p).Run(toolContext(), toolInput(BashParams{Command: "head -c 100000 /dev/zero | tr '\\0' x"}))
	require.NoError(t, err)
	var metadata BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.True(t, metadata.Truncated)
	require.NotContains(t, response.Content, "[Command output saved as artifact]", "a partial artifact must not claim to be complete")
}
