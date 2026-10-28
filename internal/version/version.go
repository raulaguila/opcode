package version

import (
	"runtime/debug"
	"strings"
)

// Release builds replace Version via -ldflags. Source builds remain identifiable.
const developmentVersion = "0.2.0-dev"

var Version = developmentVersion

func buildVersion(configured string, info *debug.BuildInfo) string {
	if configured != developmentVersion {
		return configured
	}
	if info == nil {
		return developmentVersion
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	revision, dirty := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	result := developmentVersion
	if len(revision) >= 12 && strings.Trim(revision, "0123456789abcdef") == "" {
		result += "." + revision[:12]
	}
	if dirty {
		result += ".dirty"
	}
	return result
}
func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		Version = buildVersion(Version, info)
	}
}
