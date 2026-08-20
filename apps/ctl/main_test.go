package main

import (
	"path/filepath"
	"testing"

	"github.com/argalla/taskkeeper/packages/store"
)

func nuevaDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "tk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func tareaSimple(t *testing.T, db *store.DB, nombre, dependeDe string) string {
	t.Helper()
	pid, _ := db.UpsertProject(store.Project{Name: "d", WorkspacePath: "C:\\d", GitRoot: "C:\\d", DefaultBranch: "main"})
	trigger := ""
	if dependeDe != "" {
		trigger = "success"
	}
	task, _, err := db.CreateTask(store.Task{
		Name: nombre, ProjectID: pid, Agent: "claude", Enabled: true, ConversationMode: "new",
		ScheduleRule: `{"type":"daily","time":"03:00"}`, Timezone: "Europe/Madrid",
		MisfirePolicy: "skip", PermissionProfile: "auditoria", TimeoutSeconds: 600,
		DependsOnTaskID: dependeDe, TriggerOn: trigger,
	}, "p")
	if err != nil {
		t.Fatal(err)
	}
	return task.ID
}

// §27.4: la validación de dependencias rechaza ciclos, auto-dependencia y padres
// inexistentes, y normaliza disparar-en.
func TestValidarDependencia(t *testing.T) {
	db := nuevaDB(t)
	a := tareaSimple(t, db, "A", "")
	b := tareaSimple(t, db, "B", a) // B depende de A

	o := opcionesTarea{dependeDe: a}
	if err := validarDependencia(db, &o, ""); err != nil {
		t.Errorf("depender de A debería valer: %v", err)
	}
	if o.dispararEn != "success" {
		t.Errorf("dispararEn por defecto = %q, quiero success", o.dispararEn)
	}

	o = opcionesTarea{dependeDe: b}
	if err := validarDependencia(db, &o, a); err == nil {
		t.Error("editar A para depender de B (A→B→A) debería rechazarse como ciclo")
	}

	o = opcionesTarea{dependeDe: a}
	if err := validarDependencia(db, &o, a); err == nil {
		t.Error("auto-dependencia debería rechazarse")
	}

	o = opcionesTarea{dependeDe: "noexiste"}
	if err := validarDependencia(db, &o, ""); err == nil {
		t.Error("padre inexistente debería rechazarse")
	}

	o = opcionesTarea{dependeDe: a, dispararEn: "quizas"}
	if err := validarDependencia(db, &o, ""); err == nil {
		t.Error("disparar-en inválido debería rechazarse")
	}

	o = opcionesTarea{dependeDe: "", dispararEn: "success"}
	if err := validarDependencia(db, &o, ""); err != nil {
		t.Errorf("sin dependencia no debería fallar: %v", err)
	}
	if o.dispararEn != "" {
		t.Errorf("sin dependencia, dispararEn debería quedar vacío, es %q", o.dispararEn)
	}
}

// Borrar un padre con dependientes se bloquea (no dejarlas huérfanas).
func TestCuentaDependientesBloqueaBorrado(t *testing.T) {
	db := nuevaDB(t)
	a := tareaSimple(t, db, "A", "")
	tareaSimple(t, db, "B", a)
	n, err := db.CuentaDependientes(a)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("CuentaDependientes(A) = %d, quiero 1", n)
	}
}
