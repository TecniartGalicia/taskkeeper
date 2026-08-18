// Package platform aísla todo lo que depende del sistema operativo. Nada fuera
// de aquí debe preguntar por runtime.GOOS.
package platform

import "os/exec"

// GrupoProcesos agrupa un agente y toda su descendencia para poder matarla
// entera. En Windows es un Job Object; en macOS será un grupo de procesos.
type GrupoProcesos interface {
	Prepare(cmd *exec.Cmd)
	Adopt(pid int) error
	KillAll() error
	Close() error
}
