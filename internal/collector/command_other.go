//go:build !unix

package collector

import "os/exec"

// killProcessGroupOnCancel mantém o cancelamento padrão fora de sistemas Unix.
func killProcessGroupOnCancel(*exec.Cmd) {}
