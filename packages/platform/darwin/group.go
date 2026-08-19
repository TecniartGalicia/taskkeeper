//go:build darwin

// Package darwin es la implementación de la capa de plataforma para macOS.
// Nada fuera de packages/platform debe importarlo.
package darwin

import (
	"os/exec"
	"syscall"
)

// Job es el equivalente al Job Object de Windows: agrupa el agente y su
// descendencia para poder matarla entera.
//
// El agente lidera su propio grupo de procesos (Setpgid) y se mata con una
// señal al PID negativo, que alcanza a los nietos.
//
// Asimetría reconocida (plan v2, §10): un proceso que llame a setsid() se sale
// del grupo y sobrevive. No hay equivalente exacto al Job Object. P-16 debe
// medirlo en hardware real; si aparece, se añade un barrido por PPID.
type Job struct{ pgid int }

func NewJob() (*Job, error) { return &Job{}, nil }

func (j *Job) Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func (j *Job) Adopt(pid int) error {
	j.pgid = pid // el hijo lideró su grupo: su pgid es su propio PID
	return nil
}

func (j *Job) KillAll() error {
	if j.pgid == 0 {
		return nil
	}
	return syscall.Kill(-j.pgid, syscall.SIGKILL)
}

func (j *Job) Close() error { return j.KillAll() }

// Alive comprueba existencia con la señal 0. Solo para pruebas y diagnóstico.
func Alive(pid int) bool { return syscall.Kill(pid, 0) == nil }
