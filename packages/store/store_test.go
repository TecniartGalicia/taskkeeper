package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func nuevaBase(t *testing.T) (*DB, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "taskkeeper.db")
	db, err := Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, p
}

func tareaDePrueba(t *testing.T, db *DB) (*Task, *Revision) {
	t.Helper()
	pid, err := db.UpsertProject(Project{
		Name: "demo", WorkspacePath: "C:\\demo", GitRoot: "C:\\demo", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	task, rev, err := db.CreateTask(Task{
		Name: "mantenimiento", ProjectID: pid, Agent: "claude", Enabled: true,
		ConversationMode: "new", ScheduleRule: `{"type":"daily","time":"03:00"}`,
		Timezone: "Europe/Madrid", MisfirePolicy: "skip",
		PermissionProfile: "auditoria", TimeoutSeconds: 600,
	}, "revisa las dependencias")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task, rev
}

// FR-016: una ocurrencia, una ejecución. Es la barrera que impide que un
// disparo de recuperación y el disparo normal se dupliquen.
func TestIdempotenciaUnaEjecucionPorOcurrencia(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)
	cuando := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

	r1, creada, err := db.CreateRunIfAbsent(task.ID, rev.ID, cuando)
	if err != nil || !creada || r1 == nil {
		t.Fatalf("la primera debería crearse: creada=%v err=%v", creada, err)
	}

	r2, creada2, err := db.CreateRunIfAbsent(task.ID, rev.ID, cuando)
	if err != nil {
		t.Fatalf("la segunda no debe dar error, solo no crear: %v", err)
	}
	if creada2 || r2 != nil {
		t.Fatalf("se creó una segunda ejecución para la misma ocurrencia")
	}

	// Otra ocurrencia sí crea.
	if _, creada3, err := db.CreateRunIfAbsent(task.ID, rev.ID, cuando.Add(24*time.Hour)); err != nil || !creada3 {
		t.Fatalf("otra ocurrencia debería crear: creada=%v err=%v", creada3, err)
	}
}

// La máquina de estados vive en el código: una transición imposible se rechaza.
func TestMaquinaDeEstados(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)
	run, _, _ := db.CreateRunIfAbsent(task.ID, rev.ID, time.Now().UTC())

	// queued → running no está permitido: falta el preflight.
	if err := db.Transition(run.ID, StateRunning, ""); !errors.Is(err, ErrTransicionInvalida) {
		t.Errorf("queued→running debería rechazarse, dio: %v", err)
	}
	for _, s := range []State{StatePreflight, StateRunning, StateVerifying, StateAwaitingReview} {
		if err := db.Transition(run.ID, s, ""); err != nil {
			t.Fatalf("transición legítima a %s falló: %v", s, err)
		}
	}
	// Un estado terminal ya no admite nada.
	if err := db.Transition(run.ID, StateRunning, ""); err == nil {
		t.Errorf("awaiting_review→running debería rechazarse")
	}

	got, _ := db.GetRun(run.ID)
	if got.Status != StateAwaitingReview {
		t.Errorf("estado final = %s", got.Status)
	}
	var fin sql.NullString
	db.QueryRow(`SELECT finished_at FROM runs WHERE id=?`, run.ID).Scan(&fin)
	if !fin.Valid {
		t.Errorf("un estado terminal debe fechar finished_at")
	}
}

// D7: la cancelación viaja por la base, no por un canal.
func TestCancelacionPorBandera(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)
	run, _, _ := db.CreateRunIfAbsent(task.ID, rev.ID, time.Now().UTC())

	if db.CancelRequested(run.ID) {
		t.Fatalf("no debería estar cancelada de inicio")
	}
	if err := db.RequestCancel(run.ID); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if !db.CancelRequested(run.ID) {
		t.Fatalf("la bandera de cancelación no se vio")
	}
}

// La bandeja de la mañana solo muestra lo que espera decisión.
func TestBandeja(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)

	base := time.Date(2026, 8, 25, 3, 0, 0, 0, time.UTC)
	for i, estados := range [][]State{
		{StatePreflight, StateRunning, StateVerifying, StateAwaitingReview}, // sale
		{StatePreflight, StateRunning, StateFailedAuth},                     // sale
		{StateSkipped}, // no sale
	} {
		r, _, _ := db.CreateRunIfAbsent(task.ID, rev.ID, base.Add(time.Duration(i)*time.Hour))
		for _, s := range estados {
			if err := db.Transition(r.ID, s, ""); err != nil {
				t.Fatalf("preparando: %v", err)
			}
		}
	}

	items, err := db.Inbox()
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("la bandeja devolvió %d entradas, se esperaban 2", len(items))
	}
	for _, it := range items {
		if it.Status == StateSkipped {
			t.Errorf("una ejecución omitida no debe aparecer en la bandeja")
		}
		if it.TaskName == "" || it.ProjectName == "" {
			t.Errorf("la bandeja debe traer nombre de tarea y proyecto")
		}
	}
}

// P-22. Varios procesos escribiendo a la vez no corrompen la base ni pierden
// escrituras. Es lo que sustituye al "escritor único" del diseño con demonio.
func TestEscritoresConcurrentesEntreProcesos(t *testing.T) {
	db, ruta := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)

	const procesos, porProceso = 4, 15

	var wg sync.WaitGroup
	errs := make(chan string, procesos)
	for p := 0; p < procesos; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=TestAyudanteEscribe")
			cmd.Env = append(os.Environ(),
				"TK_ROL=escritor", "TK_DB="+ruta, "TK_TASK="+task.ID,
				"TK_REV="+rev.ID, "TK_BASE="+strconv.Itoa(p*1000),
				"TK_N="+strconv.Itoa(porProceso))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Sprintf("proceso %d: %v · %s", p, err, out)
			}
		}(p)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("un escritor falló: %s", e)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&n); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if n != procesos*porProceso {
		t.Fatalf("se guardaron %d ejecuciones, se esperaban %d", n, procesos*porProceso)
	}

	// La base sigue íntegra.
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		t.Fatalf("integridad de la base: %q (%v)", res, err)
	}
}

func TestAyudanteEscribe(t *testing.T) {
	if os.Getenv("TK_ROL") != "escritor" {
		t.Skip("no es el escritor")
	}
	db, err := Open(os.Getenv("TK_DB"))
	if err != nil {
		t.Fatalf("abrir: %v", err)
	}
	defer db.Close()

	base, _ := strconv.Atoi(os.Getenv("TK_BASE"))
	n, _ := strconv.Atoi(os.Getenv("TK_N"))
	taskID, revID := os.Getenv("TK_TASK"), os.Getenv("TK_REV")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < n; i++ {
		cuando := t0.Add(time.Duration(base+i) * time.Minute)
		run, creada, err := db.CreateRunIfAbsent(taskID, revID, cuando)
		if err != nil {
			t.Fatalf("CreateRunIfAbsent: %v", err)
		}
		if !creada {
			continue
		}
		if err := db.AppendEvent(run.ID, "log", `{"t":"hola"}`); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		if err := db.Transition(run.ID, StatePreflight, ""); err != nil {
			t.Fatalf("Transition: %v", err)
		}
	}
}

// Con una sola conexión, consultar dentro de un cursor abierto se bloquea para
// siempre. Este caso lo destapó la prueba de humo de la línea de órdenes, no la
// suite: la Fase 1 nunca listó una base con más de una tarea.
func TestListarVariasTareasNoSeBloquea(t *testing.T) {
	db, _ := nuevaBase(t)
	pid, _ := db.UpsertProject(Project{Name: "d", WorkspacePath: "C:/d", GitRoot: "C:/d", DefaultBranch: "main"})
	for i := 0; i < 3; i++ {
		if _, _, err := db.CreateTask(Task{
			Name: fmt.Sprintf("t%d", i), ProjectID: pid, Agent: "claude", Enabled: true,
			ConversationMode: "new", ScheduleRule: `{"type":"daily","time":"03:00"}`,
			Timezone: "Europe/Madrid", MisfirePolicy: "skip", PermissionProfile: "auditoria",
			TimeoutSeconds: 60,
		}, "p"); err != nil {
			t.Fatal(err)
		}
	}
	hecho := make(chan int, 1)
	go func() {
		ts, err := db.ListTasks()
		if err != nil {
			hecho <- -1
			return
		}
		hecho <- len(ts)
	}()
	select {
	case n := <-hecho:
		if n != 3 {
			t.Fatalf("ListTasks devolvió %d, se esperaban 3", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("ListTasks se bloqueó con varias tareas")
	}
}
