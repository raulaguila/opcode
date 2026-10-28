package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/viper"
)

// Legacy names live here. Offline reads and project/SQLite paths retain their
// original locations; normal startup adopts global JSON in the canonical path.
const legacyAppName = "opencode"
const legacyEnvironmentPrefix = "OPENCODE_"
const currentEnvironmentPrefix = "OPCODE_"

func preferredExisting(current, legacy string) string {
	if _, err := os.Lstat(current); !os.IsNotExist(err) {
		return current
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		return legacy
	}
	return current
}
func ProjectConfigPath(root string) string {
	current := filepath.Join(root, "."+appName, "config.json")
	for _, path := range []string{current, filepath.Join(root, "."+appName+".json"), filepath.Join(root, "."+legacyAppName+".json")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return path
		}
	}
	return current
}
func DatabasePath(dataDir string) string {
	return preferredExisting(filepath.Join(dataDir, appName+".db"), filepath.Join(dataDir, legacyAppName+".db"))
}
func defaultDataPath(root string) string {
	current, legacy := filepath.Join(root, "."+appName), filepath.Join(root, "."+legacyAppName)
	// A new configuration-only directory must not hide an existing database.
	for _, directory := range []string{current, legacy} {
		for _, name := range []string{appName + ".db", legacyAppName + ".db"} {
			if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				return directory
			}
		}
	}
	return preferredExisting(current, legacy)
}
func globalConfigPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	current := filepath.Join(home, "."+appName, "config.json")
	if _, err := os.Lstat(current); !os.IsNotExist(err) {
		return current, true
	}
	for _, name := range []string{appName, legacyAppName} {
		for _, path := range []string{filepath.Join(home, "."+name+".json"), filepath.Join(xdg, name, "."+name+".json"), filepath.Join(home, ".config", name, "."+name+".json")} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return path, true
			}
		}
	}
	return current, false
}

// LookupEnvironmentWith gives the new alias precedence, including an explicitly
// empty value. Provider names such as OPENAI_API_KEY are unchanged.
func EnvironmentAliases(name string) []string {
	if strings.HasPrefix(name, currentEnvironmentPrefix) {
		return []string{name, legacyEnvironmentPrefix + strings.TrimPrefix(name, currentEnvironmentPrefix)}
	}
	if strings.HasPrefix(name, legacyEnvironmentPrefix) {
		return []string{currentEnvironmentPrefix + strings.TrimPrefix(name, legacyEnvironmentPrefix), name}
	}
	return []string{name}
}
func LookupEnvironmentWith(name string, lookup func(string) (string, bool)) (string, bool) {
	for _, alias := range EnvironmentAliases(name) {
		if value, ok := lookup(alias); ok {
			return value, true
		}
	}
	return "", false
}

func LookupEnvironment(name string) (string, bool) { return LookupEnvironmentWith(name, os.LookupEnv) }
func Environment(name string) string               { value, _ := LookupEnvironment(name); return value }
func bindCompatibleEnvironment(v *viper.Viper) {
	keys := map[string]bool{}
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, prefix string) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			key := prefix + name
			if field.Type.Kind() == reflect.Struct {
				walk(field.Type, key+".")
			} else {
				keys[key] = true
			}
		}
	}
	walk(reflect.TypeOf(Config{}), "")
	for _, key := range v.AllKeys() {
		keys[key] = true
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		for _, prefix := range []string{currentEnvironmentPrefix, legacyEnvironmentPrefix} {
			if strings.HasPrefix(name, prefix) {
				keys[strings.ToLower(strings.TrimPrefix(name, prefix))] = true
			}
		}
	}
	for key := range keys {
		upper := strings.ToUpper(key)
		underscored := strings.ReplaceAll(upper, ".", "_")
		_ = v.BindEnv(key, currentEnvironmentPrefix+upper, currentEnvironmentPrefix+underscored, legacyEnvironmentPrefix+upper, legacyEnvironmentPrefix+underscored)
	}
}

// LegacySensitivePath is shared by read tools and sandbox masking. Legacy
// storage stays protected even when the new data directory is selected.
func LegacySensitivePath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.EqualFold(part, "."+legacyAppName) {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(path))
	return base == "."+legacyAppName+".json" || base == legacyAppName+".json" || base == "."+legacyAppName+"-integration.json"
}

// IntegrationConfigPath retains the user's existing private provider-test file.
func IntegrationConfigPath(configRoot string) string {
	return preferredExisting(filepath.Join(configRoot, appName, "integration.json"), filepath.Join(configRoot, legacyAppName, "integration.json"))
}

func IsLegacyEnvironment(name string) bool { return strings.HasPrefix(name, legacyEnvironmentPrefix) }

func legacyGuidancePaths() []string {
	return []string{"opencode.md", "opencode.local.md", "OpenCode.md", "OpenCode.local.md", "OPENCODE.md", "OPENCODE.local.md"}
}
func defaultGuidancePaths(root string) []string {
	paths := append([]string(nil), defaultContextPaths...)
	for _, local := range []bool{false, true} {
		suffix := ".md"
		if local {
			suffix = ".local.md"
		}
		hasCurrent := false
		for _, base := range []string{"opcode", "Opcode", "OPCODE"} {
			if _, err := os.Lstat(filepath.Join(root, base+suffix)); err == nil {
				hasCurrent = true
				break
			}
		}
		if !hasCurrent {
			for _, legacy := range legacyGuidancePaths() {
				if strings.HasSuffix(legacy, ".local.md") == local {
					paths = append(paths, legacy)
				}
			}
		}
	}
	return paths
}
