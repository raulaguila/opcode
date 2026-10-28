package version

import (
	"github.com/stretchr/testify/require"
	"runtime/debug"
	"testing"
)

func TestBuildVersionPreservesReleaseAndIdentifiesSource(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}
	require.Equal(t, "0.2.0-dev.0123456789ab.dirty", buildVersion(developmentVersion, info))
	require.Equal(t, "0.2.0-beta.1", buildVersion("0.2.0-beta.1", info))
	require.Equal(t, developmentVersion, buildVersion(developmentVersion, nil))
	info.Main.Version = "v0.2.0"
	require.Equal(t, "v0.2.0", buildVersion(developmentVersion, info))
}
