//go:build !windows

package cli

import "syscall"

func execProcess(argv0 string, argv []string, env []string) error {
	return syscall.Exec(argv0, argv, env)
}
