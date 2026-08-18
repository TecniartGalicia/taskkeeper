//go:build windows

package platform

import (
	"fmt"
	win "github.com/argalla/taskkeeper/packages/platform/windows"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type EspecDisparador struct {
	Tipo     string
	Inicio   time.Time
	Weekdays []int
}

func RegistrarTarea(taskID, comando, argumentos string, spec EspecDisparador) error {
	return win.Register(taskID, comando, argumentos, win.TriggerSpec{
		Tipo: spec.Tipo, Inicio: spec.Inicio, Weekdays: spec.Weekdays,
	})
}
func RetirarTarea(taskID string) error   { return win.Unregister(taskID) }
func NombreDeTarea(taskID string) string { return win.NombreTarea(taskID) }

// LanzarWorker arranca una ejecución a mano y DEVUELVE EL CONTROL AL INSTANTE.
//
// Antes esperaba a que terminase, así que "Ejecutar ahora" dejaba la extensión
// colgada todo lo que durase el agente. Se lanza desatendido y el progreso se
// sigue por la bandeja, igual que en una ejecución nocturna.
//
// No se usa el disparo del programador para esto a propósito: por ahí llegaría
// con los argumentos registrados y el worker derivaría la hora prevista, que es
// justo lo contrario de lo que quiere decir "ahora".
func LanzarWorker(worker, taskID string, cuando time.Time) error {
	cmd := exec.Command(worker, "--run", taskID, "--manual",
		"--occurrence", cuando.Format(time.RFC3339))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Se suelta el proceso: vive por su cuenta y este mando se va.
	return cmd.Process.Release()
}

// AvisoReactivacion devuelve un texto para el usuario si el equipo no va a poder
// despertar a la hora. Medido en la Fase 0: con batería viene desactivado de
// fábrica, y hay un tercer estado, "solo temporizadores importantes", que
// tampoco sirve.
func AvisoReactivacion() string {
	out, err := exec.Command("powercfg", "/q", "SCHEME_CURRENT", "SUB_SLEEP", "RTCWAKE").Output()
	if err != nil {
		return ""
	}
	ac, dc := indices(string(out))
	switch {
	case ac == 1 && dc == 1:
		return ""
	case ac == 1 && dc != 1:
		return "con batería este equipo no despertará para ejecutar la tarea; " +
			"solo lo hará con el cable conectado"
	default:
		return "los temporizadores de reactivación están desactivados: el equipo " +
			"no despertará, la tarea se ejecutará cuando lo enciendas"
	}
}
func indices(s string) (ac, dc int) {
	ac, dc = -1, -1
	for _, l := range strings.Split(s, "\n") {
		low := strings.ToLower(l)
		var v int
		if i := strings.Index(low, "0x"); i >= 0 {
			fmt.Sscanf(strings.TrimSpace(low[i:]), "0x%x", &v)
		} else {
			continue
		}
		if strings.Contains(low, "alterna") || strings.Contains(low, " ac ") {
			ac = v
		} else if strings.Contains(low, "continua") || strings.Contains(low, " dc ") {
			dc = v
		}
	}
	return ac, dc
}
