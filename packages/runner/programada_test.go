//go:build windows || darwin

package runner

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/argalla/taskkeeper/packages/scheduler"
	"github.com/argalla/taskkeeper/packages/store"
)

// Ajusta la tarea creada por montar() para las pruebas de retraso.
func configurarRetraso(t *testing.T, db *store.DB, taskID, politica string, maxSeg int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE tasks SET misfire_policy=?, max_lateness_seconds=? WHERE id=?`,
		politica, maxSeg, taskID); err != nil {
		t.Fatal(err)
	}
}

// El worker deriva de qué hora viene en vez de suponer "ahora". Sin esto no
// puede saber si llega tarde ni proteger contra duplicados.
func TestProgramadaDerivaLaHoraPrevista(t *testing.T) {
	db, taskID, _, o, d := montar(t, "trabaja", "auditoria")
	configurarRetraso(t, db, taskID, "run_if_late", 7200)

	// La tarea es diaria a las 03:00 de Madrid. Fingimos que el worker arranca a
	// las 03:00:30, es decir, a su hora.
	task, _ := db.GetTask(taskID)
	var regla scheduler.Rule
	json.Unmarshal([]byte(task.ScheduleRule), &regla)
	prevista, err := scheduler.Previous(regla, task.Timezone, time.Now().UTC())
	if err != nil || prevista == nil {
		t.Fatalf("Previous: %v", err)
	}
	ahora := prevista.ScheduledForUTC.Add(30 * time.Second)

	if err := EjecutarProgramada(context.Background(), db, d, o, taskID, ahora); err != nil {
		t.Fatalf("EjecutarProgramada: %v", err)
	}

	// La ejecución debe quedar guardada con la HORA PREVISTA, no con "ahora".
	var guardada string
	if err := db.QueryRow(`SELECT scheduled_for_utc FROM runs WHERE task_id=?`, taskID).
		Scan(&guardada); err != nil {
		t.Fatalf("no se creó la ejecución: %v", err)
	}
	if guardada != prevista.ScheduledForUTC.Format(time.RFC3339) {
		t.Errorf("se guardó %s; se esperaba la hora prevista %s",
			guardada, prevista.ScheduledForUTC.Format(time.RFC3339))
	}
}

// Y de ahí sale lo importante: con la hora derivada, un disparo de recuperación
// y el disparo normal son la MISMA ocurrencia, así que no se duplican. Antes,
// con "ahora", la protección no protegía nada en las ejecuciones reales.
func TestProgramadaNoDuplicaEntreDisparoNormalYRecuperacion(t *testing.T) {
	db, taskID, _, o, d := montar(t, "trabaja", "auditoria")
	configurarRetraso(t, db, taskID, "run_if_late", 7200)

	task, _ := db.GetTask(taskID)
	var regla scheduler.Rule
	json.Unmarshal([]byte(task.ScheduleRule), &regla)
	prevista, _ := scheduler.Previous(regla, task.Timezone, time.Now().UTC())

	// Disparo a su hora, y otro media hora después, como si el sistema hubiera
	// recuperado la ejecución perdida.
	for _, ahora := range []time.Time{
		prevista.ScheduledForUTC.Add(10 * time.Second),
		prevista.ScheduledForUTC.Add(30 * time.Minute),
	} {
		if err := EjecutarProgramada(context.Background(), db, d, o, taskID, ahora); err != nil {
			t.Fatalf("EjecutarProgramada: %v", err)
		}
	}

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM runs WHERE task_id=?`, taskID).Scan(&n)
	if n != 1 {
		t.Fatalf("se crearon %d ejecuciones; el disparo de recuperación duplicó la ocurrencia", n)
	}
}

// La política del usuario vuelve a decidir. Antes ganaba siempre la de Windows
// y este campo se guardaba sin que nadie lo leyera.
func TestLaPoliticaDeRetrasoSeAplica(t *testing.T) {
	casos := []struct {
		politica string
		maxSeg   int
		retraso  time.Duration
		espera   store.State
		aparece  bool
	}{
		{"skip", 0, 5 * time.Hour, store.StateSkipped, false},
		{"run_if_late", 7200, 1 * time.Hour, store.StateAwaitingReview, true},
		{"run_if_late", 7200, 5 * time.Hour, store.StateSkipped, false},
		{"manual", 0, 5 * time.Hour, store.StateQueued, false},
	}

	for _, c := range casos {
		t.Run(c.politica+"/"+c.retraso.String(), func(t *testing.T) {
			db, taskID, _, o, d := montar(t, "trabaja", "auditoria")
			configurarRetraso(t, db, taskID, c.politica, c.maxSeg)

			task, _ := db.GetTask(taskID)
			var regla scheduler.Rule
			json.Unmarshal([]byte(task.ScheduleRule), &regla)
			prevista, _ := scheduler.Previous(regla, task.Timezone, time.Now().UTC())
			ahora := prevista.ScheduledForUTC.Add(c.retraso)

			if err := EjecutarProgramada(context.Background(), db, d, o, taskID, ahora); err != nil {
				t.Fatalf("EjecutarProgramada: %v", err)
			}

			var estado string
			if err := db.QueryRow(`SELECT status FROM runs WHERE task_id=?`, taskID).Scan(&estado); err != nil {
				t.Fatalf("no quedó constancia de la ocurrencia: %v", err)
			}
			if store.State(estado) != c.espera {
				t.Errorf("estado = %s, se esperaba %s", estado, c.espera)
			}

			// Lo omitido no ensucia la bandeja; lo ejecutado sí aparece.
			items, _ := db.Inbox()
			if c.aparece && len(items) == 0 {
				t.Errorf("debería aparecer en la bandeja")
			}
			if !c.aparece && len(items) != 0 {
				t.Errorf("no debería aparecer en la bandeja: %+v", items)
			}
		})
	}
}

// Una tarea pausada no se ejecuta aunque el disparador siga registrado.
func TestTareaPausadaNoSeEjecuta(t *testing.T) {
	db, taskID, _, o, d := montar(t, "trabaja", "auditoria")
	if _, err := db.Exec(`UPDATE tasks SET enabled=0 WHERE id=?`, taskID); err != nil {
		t.Fatal(err)
	}
	if err := EjecutarProgramada(context.Background(), db, d, o, taskID, time.Now().UTC()); err != nil {
		t.Fatalf("EjecutarProgramada: %v", err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM runs WHERE task_id=?`, taskID).Scan(&n)
	if n != 0 {
		t.Errorf("una tarea pausada creó %d ejecuciones", n)
	}
}

// El cupo es un ajuste de la máquina y vive en la base: cambiarlo no obliga a
// tocar ningún disparador.
func TestCupoViveEnLaBase(t *testing.T) {
	db, _, _, _, _ := montar(t, "trabaja", "auditoria")

	if got := db.Cupo(); got != 1 {
		t.Errorf("cupo por defecto = %d, se esperaba 1", got)
	}
	if err := db.FijarAjuste(store.ClaveCupo, "3"); err != nil {
		t.Fatal(err)
	}
	if got := db.Cupo(); got != 3 {
		t.Errorf("cupo tras fijarlo = %d, se esperaba 3", got)
	}
	// Un valor absurdo no rompe nada: se cae al mínimo seguro.
	db.FijarAjuste(store.ClaveCupo, "no-es-un-numero")
	if got := db.Cupo(); got != 1 {
		t.Errorf("con un valor inválido el cupo debería ser 1, fue %d", got)
	}
}

// Cuota agotada: se clasifica como failed_quota (no como fallo generico), se
// programa UN reintento a la hora que dice el proveedor, y un segundo fallo por
// cuota en las mismas 24 h no encadena otro reintento.
func TestCuotaAgotadaProgramaUnSoloReintento(t *testing.T) {
	db, taskID, _, o, d := montar(t, "cuota", "auditoria")

	var registrados []time.Time
	o.RegistrarReintento = func(id string, cuando time.Time) error {
		if id != taskID {
			t.Errorf("reintento para otra tarea: %s", id)
		}
		registrados = append(registrados, cuando)
		return nil
	}

	for i := 0; i < 2; i++ {
		if err := Ejecutar(context.Background(), db, d, o, taskID, time.Now().UTC().Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("Ejecutar #%d: %v", i+1, err)
		}
	}

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM runs WHERE task_id=? AND status='failed_quota'`, taskID).Scan(&n)
	if n != 2 {
		t.Fatalf("se esperaban 2 ejecuciones en failed_quota, hay %d", n)
	}
	if len(registrados) != 1 {
		t.Fatalf("se programaron %d reintentos; debe ser exactamente 1", len(registrados))
	}
	// El agente falso dice "try again at" dentro de una hora: el reintento va
	// justo despues, no dentro de la espera por defecto.
	if d := time.Until(registrados[0]); d < 55*time.Minute || d > 70*time.Minute {
		t.Errorf("el reintento no respeta la hora del proveedor: dentro de %s", d.Round(time.Minute))
	}
	var reset string
	db.QueryRow(`SELECT quota_reset_at FROM runs WHERE task_id=? ORDER BY rowid LIMIT 1`, taskID).Scan(&reset)
	if reset == "" {
		t.Errorf("no se guardo quota_reset_at")
	}
}
