//go:build windows

package cli

import (
	"os"
	"os/exec"
)

func execProcess(argv0 string, argv []string, env []string) error {
	c := exec.Command(argv0, argv[1:]...)
	c.Env = env
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
