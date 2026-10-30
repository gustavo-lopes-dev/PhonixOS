//go:build unix

package collector

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel executa o comando num grupo de processos próprio e,
// no cancelamento, envia SIGKILL ao grupo inteiro (inclui netos órfãos).
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
