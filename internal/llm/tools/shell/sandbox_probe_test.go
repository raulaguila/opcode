package shell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrokenSandboxExplainsItselfAndIsProbedOnce(t *testing.T) {
	bin, runs := t.TempDir(), filepath.Join(t.TempDir(), "runs")
	script := "#!/bin/sh\necho run >> " + runs + "\necho 'bwrap: setting up uid map: Permission denied' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bwrap"), []byte(script), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	require.ErrorContains(t, sandboxStartError(), "setting up uid map: Permission denied")
	require.ErrorContains(t, sandboxStartError(), "sandbox cannot start")
	data, err := os.ReadFile(runs)
	require.NoError(t, err)
	require.Equal(t, "run\n", string(data))
}

func TestSandboxHintNamesTheBlockingSetting(t *testing.T) {
	root := func(files map[string]string) string {
		dir := t.TempDir()
		for name, value := range files {
			path := filepath.Join(dir, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte(value), 0o600))
		}
		return dir
	}
	for hint, files := range map[string]map[string]string{
		"AppArmor":                         {"proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1\n"},
		"user.max_user_namespaces":         {"proc/sys/user/max_user_namespaces": "0\n"},
		"kernel.unprivileged_userns_clone": {"proc/sys/kernel/unprivileged_userns_clone": "0\n"},
		"container":                        {".dockerenv": "", "proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1\n"},
		"SELinux":                          {"sys/fs/selinux/enforce": "1"},
		"administrator":                    {"proc/sys/user/max_user_namespaces": "15000\n"},
	} {
		require.Contains(t, sandboxHint(root(files)), hint)
	}
}
