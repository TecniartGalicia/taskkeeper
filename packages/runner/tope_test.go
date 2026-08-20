//go:build windows

package runner

import (
	"context"
	"testing"
	"time"

	"github.com/argalla/taskkeeper/packages/store"
)

// §27.2: con un tope mensual por debajo de lo ya gastado, la siguiente ocurrencia
// se salta ANTES de tomar turno o crear worktree, y queda constancia del motivo.
func TestTopeMensualSalta(t *testing.T) {
	db, taskID, _, o, d := montar(t, "trabaja", "cambios_aislados")

	// 1) una ejecución que gasta (el agente falso informa 0.42 USD).
	if err := Ejecutar(context.Background(), db, d, o, taskID, time.Now().UTC()); err != nil {
		t.Fatalf("primera ejecución: %v", err)
	}
	inbox, _ := db.Inbox()
	if len(inbox) != 1 || inbox[0].Status != store.StateAwaitingReview {
		t.Fatalf("la primera debía quedar en awaiting_review, bandeja=%+v", inbox)
	}

	// 2) tope mensual por debajo de lo gastado.
	if err := db.FijarTopeMes(0.01); err != nil {
		t.Fatal(err)
	}

	// 3) otra ocurrencia: debe saltarse por tope, sin nueva revisión.
	if err := Ejecutar(context.Background(), db, d, o, taskID, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("segunda ejecución: %v", err)
	}
	est, _, err := db.LastRunState(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if est != string(store.StateSkipped) {
		t.Errorf("la segunda run está en %s, quiero skipped por tope", est)
	}
	// La cap no debe haber dejado una segunda run esperando revisión.
	inbox2, _ := db.Inbox()
	nAwait := 0
	for _, it := range inbox2 {
		if it.Status == store.StateAwaitingReview {
			nAwait++
		}
	}
	if nAwait != 1 {
		t.Errorf("hay %d runs esperando revisión, quiero 1 (la cap frenó la segunda)", nAwait)
	}
}

// Sin tope (0), no se frena nada: helper directo sobre topeSuperado.
func TestSinTopeNoFrena(t *testing.T) {
	db, taskID, _, _, _ := montar(t, "trabaja", "auditoria")
	task, _ := db.GetTask(taskID)
	if m := topeSuperado(db, task, time.Now().UTC()); m != "" {
		t.Errorf("sin tope no debería frenar, motivo=%q", m)
	}
}
