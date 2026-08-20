package store

import (
	"testing"
	"time"
)

// §27.1: el resumen de anoche agrupa por estado y suma el coste; la salud cuenta
// solo los disparos perdidos por MISFIRE (no los saltos por sin-turno/tope).
func TestResumenNocheYSalud(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)

	// Lleva una run por un camino de transiciones válidas y le fija el coste.
	crearRun := func(offset time.Duration, camino []State, coste float64, detalleSkip string) {
		cuando := time.Now().UTC().Add(offset)
		run, creada, err := db.CreateRunIfAbsent(task.ID, rev.ID, cuando)
		if err != nil || !creada {
			t.Fatalf("crear run: %v creada=%v", err, creada)
		}
		for _, st := range camino {
			d := ""
			if st == StateSkipped {
				d = detalleSkip
			}
			if err := db.Transition(run.ID, st, d); err != nil {
				t.Fatalf("transición a %s: %v", st, err)
			}
		}
		if coste > 0 {
			if err := db.SetRunField(run.ID, "cost_usd", coste); err != nil {
				t.Fatalf("coste: %v", err)
			}
		}
	}

	crearRun(-1*time.Hour, []State{StatePreflight, StateRunning, StateCompleted}, 0.20, "")
	crearRun(-2*time.Hour, []State{StatePreflight, StateRunning, StateVerifying, StateAwaitingReview}, 0.30, "")
	crearRun(-3*time.Hour, []State{StatePreflight, StateFailed}, 0, "")
	crearRun(-4*time.Hour, []State{StateSkipped}, 0, `{"motivo":"misfire","detalle":"retraso de 5m0s"}`) // misfire
	crearRun(-5*time.Hour, []State{StateSkipped}, 0, "sin turno libre dentro del plazo")                 // no cuenta como perdido
	// Cancelada con coste: cuenta como fallida y su coste entra en el total.
	crearRun(-6*time.Hour, []State{StatePreflight, StateRunning, StateCancelled}, 0.05, "")

	res, err := db.ResumenNoche(time.Now().UTC().Add(-16 * time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminadas != 1 {
		t.Errorf("terminadas=%d, quiero 1", res.Terminadas)
	}
	if res.EsperanRevision != 1 {
		t.Errorf("esperan_revision=%d, quiero 1", res.EsperanRevision)
	}
	if res.Fallidas != 2 { // failed + cancelled
		t.Errorf("fallidas=%d, quiero 2 (failed + cancelled)", res.Fallidas)
	}
	if res.Saltadas != 2 {
		t.Errorf("saltadas=%d, quiero 2", res.Saltadas)
	}
	if res.CosteTotalUSD < 0.54 || res.CosteTotalUSD > 0.56 { // 0.20+0.30+0.05
		t.Errorf("coste=%.2f, quiero ~0.55 (incluye la cancelada)", res.CosteTotalUSD)
	}

	salud, err := db.SaludBase(time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if len(salud) != 1 {
		t.Fatalf("salud: %d tareas, quiero 1", len(salud))
	}
	if salud[0].DisparosPerdidos != 1 {
		t.Errorf("disparos_perdidos=%d, quiero 1 (solo el de retraso, no el de sin-turno)", salud[0].DisparosPerdidos)
	}
	if salud[0].Pendientes != 0 {
		t.Errorf("pendientes=%d, quiero 0", salud[0].Pendientes)
	}
}

// La ventana de «anoche» es móvil: lo de hace 20 h no entra con --horas 16.
func TestResumenNocheVentana(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)
	// Una run terminada, pero con finished_at fuera de la ventana: se fuerza a mano.
	run, _, err := db.CreateRunIfAbsent(task.ID, rev.ID, time.Now().UTC().Add(-20*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []State{StatePreflight, StateRunning, StateCompleted} {
		db.Transition(run.ID, st, "")
	}
	// Reescribe finished_at a hace 20 h (Transition lo puso a "ahora").
	viejo := time.Now().UTC().Add(-20 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE runs SET finished_at=? WHERE id=?`, viejo, run.ID); err != nil {
		t.Fatal(err)
	}
	res, err := db.ResumenNoche(time.Now().UTC().Add(-16 * time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminadas != 0 {
		t.Errorf("terminadas=%d, quiero 0 (fuera de la ventana de 16 h)", res.Terminadas)
	}
}

// Una run atascada en `queued` de hace días NO debe contar como «en curso» eterno.
func TestResumenNocheNoCuentaQueuedHuerfana(t *testing.T) {
	db, _ := nuevaBase(t)
	task, rev := tareaDePrueba(t, db)
	// queued con scheduled_for de hace 20 h (una pendiente-manual que nunca se resolvió).
	if _, _, err := db.CreateRunIfAbsent(task.ID, rev.ID, time.Now().UTC().Add(-20*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// y una en curso reciente, que SÍ debe contar.
	run, _, err := db.CreateRunIfAbsent(task.ID, rev.ID, time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	db.Transition(run.ID, StatePreflight, "")
	db.Transition(run.ID, StateRunning, "")

	res, err := db.ResumenNoche(time.Now().UTC().Add(-16 * time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if res.EnCurso != 1 {
		t.Errorf("en_curso=%d, quiero 1 (la huérfana de -20h no cuenta)", res.EnCurso)
	}
}
