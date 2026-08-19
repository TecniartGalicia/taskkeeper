//go:build !windows && !darwin

// En plataformas sin soporte (Linux, etc.) el paquete compila para que
// `go build ./...` y las pruebas agnósticas del sistema corran en CI, pero las
// operaciones propias del sistema devuelven un error claro: TaskKeeper solo se
// distribuye para Windows (y macOS cuando se verifique en hardware).
package platform

import (
	"errors"
	"time"
)

var errNoSoportado = errors.New("taskkeeper: esta plataforma no está soportada (solo Windows y macOS)")

type EspecDisparador struct {
	Tipo     string
	Inicio   time.Time
	Horas    []time.Time
	Weekdays []int
}

func NuevoGrupo() (GrupoProcesos, error) { return nil, errNoSoportado }

func RegistrarTarea(taskID, comando, argumentos string, spec EspecDisparador) error {
	return errNoSoportado
}
func RetirarTarea(taskID string) error                                 { return errNoSoportado }
func RegistrarReintento(worker, taskID string, cuando time.Time) error { return errNoSoportado }
func NombreDeTarea(taskID string) string                               { return "taskkeeper-" + taskID }
func LanzarWorker(worker, taskID string, cuando time.Time) error       { return errNoSoportado }
func AvisoReactivacion() string                                        { return "" }
