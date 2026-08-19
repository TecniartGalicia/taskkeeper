//go:build darwin

package platform

import (
	"time"

	dwin "github.com/argalla/taskkeeper/packages/platform/darwin"
)

type EspecDisparador struct {
	Tipo     string
	Inicio   time.Time
	Weekdays []int
}

func RegistrarTarea(taskID, comando, argumentos string, spec EspecDisparador) error {
	return dwin.Register(taskID, comando, argumentos, dwin.EspecDisparador{
		Tipo: spec.Tipo, Inicio: spec.Inicio, Weekdays: spec.Weekdays,
	})
}

func RetirarTarea(taskID string) error   { return dwin.Unregister(taskID) }
func NombreDeTarea(taskID string) string { return dwin.NombreTarea(taskID) }
func AvisoReactivacion() string          { return dwin.AvisoReactivacion() }

func RegistrarReintento(worker, taskID string, cuando time.Time) error {
	return dwin.RegistrarReintento(worker, taskID, cuando)
}

func LanzarWorker(worker, taskID string, cuando time.Time) error {
	return dwin.LanzarWorker(worker, taskID, cuando)
}
