//go:build windows

// Package windows contiene la implementación de la capa de plataforma para
// Windows. Nada fuera de packages/platform debe importar este paquete ni
// preguntar por runtime.GOOS.
package windows

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job agrupa un proceso de agente y toda su descendencia.
//
// Se usa KILL_ON_JOB_CLOSE: al cerrar el último handle del objeto, Windows mata
// a todos los procesos asignados, incluidos los nietos y los que hayan quedado
// huérfanos porque su padre terminó antes. Esa es la diferencia con
// `taskkill /T`, que recorre el árbol por PID padre en el momento de la llamada
// y no encuentra a un nieto cuyo intermedio ya murió.
type Job struct{ h windows.Handle }

func NewJob() (*Job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &Job{h: h}, nil
}

// Prepare marca el proceso para que no cree una ventana de consola visible.
// Se llama antes de Start.
func (j *Job) Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &windows.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

// Adopt asigna un proceso ya arrancado al job. Se llama inmediatamente después
// de Start, en la instrucción siguiente.
//
// Ventana de carrera conocida: entre Start y Adopt el hijo podría crear un
// nieto que quedaría fuera del job. En la práctica los CLI de agente tardan
// cientos de milisegundos en arrancar. Si la ventana resultase explotable, la
// salida es sustituir os/exec por CreateProcess con CREATE_SUSPENDED, asignar
// y reanudar el hilo principal.
func (j *Job) Adopt(pid int) error {
	ph, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(j.h, ph)
}

// KillAll mata a todos los procesos del job de forma explícita.
func (j *Job) KillAll() error {
	if j.h == 0 {
		return nil
	}
	return windows.TerminateJobObject(j.h, 1)
}

// Close cierra el objeto. Por KILL_ON_JOB_CLOSE, esto mata por sí solo a toda
// la descendencia: es la red de seguridad si el runner cae sin cancelar nada.
func (j *Job) Close() error {
	if j.h == 0 {
		return nil
	}
	h := j.h
	j.h = 0
	return windows.CloseHandle(h)
}

// Alive indica si un PID sigue vivo. Solo para pruebas y diagnóstico: la
// reutilización de PID por parte de Windows lo hace inseguro para lógica real.
func Alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}
