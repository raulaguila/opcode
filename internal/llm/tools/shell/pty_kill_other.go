//go:build !linux && !windows

package shell

func killPTYSession(int) {}
