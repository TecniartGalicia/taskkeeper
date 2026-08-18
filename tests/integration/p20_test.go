//go:build windows

// P-20. La prueba que valida la decisión de arquitectura más importante del
// plan v2: que sea el programador del sistema operativo quien dispare, y no un
// demonio nuestro.
//
// Comprueba la cadena completa: registrar el disparador → el sistema lanza el
// worker con sus argumentos → el worker abre la base y deja constancia.
//
// No se usa ningún agente real: la tarea se crea con un agente inexistente, así
// que la ejecución llega hasta el preflight y falla ahí. Lo que se está midiendo
// es el disparo, no el agente, y de paso no se gasta cuota.
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argalla/taskkeeper/packages/platform"
	"github.com/argalla/taskkeeper/packages/store"
)

func TestElSistemaOperativoDisparaElWorker(t *testing.T) {
	worker := construirWorker(t)
	home := t.TempDir()

	// Tarea con un agente que no existe: la ejecución llegará al preflight.
	db, err := store.Open(filepath.Join(home, "taskkeeper.db"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := db.UpsertProject(store.Project{
		Name: "demo", WorkspacePath: home, GitRoot: home, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	// La política de retraso es run_if_late con una ventana enorme a propósito:
	// el worker deriva la hora prevista de la regla, y a cualquier hora del día
	// que se ejecute la prueba llegaría "tarde" respecto a las 03:00. Lo que se
	// mide aquí es el disparo, no la política, que tiene sus propias pruebas.
	task, _, err := db.CreateTask(store.Task{
		Name: "sonda p20", ProjectID: pid, Agent: "agente-inexistente", Enabled: true,
		ConversationMode: "new", ScheduleRule: `{"type":"daily","time":"03:00"}`,
		Timezone: "Europe/Madrid", MisfirePolicy: "run_if_late",
		MaxLatenessSeconds: 30 * 24 * 3600,
		PermissionProfile:  "auditoria", TimeoutSeconds: 60,
	}, "no importa")
	if err != nil {
		t.Fatal(err)
	}
	db.Close() // el worker abrirá la suya: son procesos distintos

	// Registrar el disparador. La hora es irrelevante: lo forzamos con /Run.
	spec := platform.EspecDisparador{Tipo: "daily", Inicio: time.Now().Add(24 * time.Hour)}
	args := "--run " + task.ID + ` --home "` + home + `"`
	if err := platform.RegistrarTarea(task.ID, worker, args, spec); err != nil {
		t.Fatalf("registrando el disparador: %v", err)
	}
	t.Cleanup(func() { platform.RetirarTarea(task.ID) })

	// Que dispare el sistema, no nosotros.
	nombre := platform.NombreDeTarea(task.ID)
	if out, err := exec.Command("schtasks", "/Run", "/TN", nombre).CombinedOutput(); err != nil {
		t.Fatalf("schtasks /Run: %v · %s", err, out)
	}

	// Esperar a que aparezca la constancia en la base.
	db2, err := store.Open(filepath.Join(home, "taskkeeper.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	limite := time.Now().Add(45 * time.Second)
	var estado, runID string
	for time.Now().Before(limite) {
		err := db2.QueryRow(`SELECT id, status FROM runs WHERE task_id=? ORDER BY rowid DESC LIMIT 1`,
			task.ID).Scan(&runID, &estado)
		if err == nil && estado != "" && estado != string(store.StateQueued) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if runID == "" {
		t.Fatalf("el sistema no llegó a lanzar el worker, o el worker no escribió nada")
	}
	t.Logf("el sistema lanzó el worker; ejecución %s en estado %s", runID[:8], estado)

	// Llegó al preflight y falló ahí por el agente inexistente: exactamente lo
	// que se esperaba, y prueba que el worker recorrió su ciclo.
	if estado != string(store.StateFailed) && estado != string(store.StateFailedAuth) {
		t.Errorf("estado = %q; se esperaba un fallo de preflight", estado)
	}

	// Y quedó evidencia, que es requisito desde el primer día.
	var n int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM audit WHERE run_id=?`, runID).Scan(&n); err != nil || n == 0 {
		t.Errorf("no se registró evidencia de la ejecución (n=%d, err=%v)", n, err)
	}
}

// La tarea que creamos aparece en nuestra carpeta y en ninguna otra.
func TestElDisparadorViveEnNuestraCarpeta(t *testing.T) {
	worker := construirWorker(t)
	id := "p20-carpeta-" + time.Now().Format("150405")

	spec := platform.EspecDisparador{Tipo: "daily", Inicio: time.Now().Add(24 * time.Hour)}
	if err := platform.RegistrarTarea(id, worker, "--version", spec); err != nil {
		t.Fatalf("RegistrarTarea: %v", err)
	}
	t.Cleanup(func() { platform.RetirarTarea(id) })

	nombre := platform.NombreDeTarea(id)
	if !strings.HasPrefix(nombre, `Argalla\TaskKeeper\`) {
		t.Errorf("la tarea no vive en nuestra carpeta: %q", nombre)
	}
	out, err := exec.Command("schtasks", "/Query", "/TN", nombre).CombinedOutput()
	if err != nil {
		t.Fatalf("la tarea registrada no se encuentra: %v · %s", err, out)
	}
}

func construirWorker(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "taskkeeper-worker.exe")
	cmd := exec.Command("go", "build", "-o", dst, "github.com/argalla/taskkeeper/apps/worker")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compilando el worker: %v · %s", err, out)
	}
	return dst
}
