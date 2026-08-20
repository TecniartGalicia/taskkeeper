package store

import (
	"reflect"
	"sort"
	"testing"
)

// §27.4: DependientesDe casa por resultado; 'always' casa con cualquiera. Y los
// campos de dependencia sobreviven al round-trip (migración + CreateTask/GetTask).
func TestDependientesDe(t *testing.T) {
	db, _ := nuevaBase(t)
	padre, _ := tareaDePrueba(t, db)

	crearDep := func(nombre, trigger string) string {
		task, _, err := db.CreateTask(Task{
			Name: nombre, ProjectID: padre.ProjectID, Agent: "claude", Enabled: true,
			ConversationMode: "new", ScheduleRule: `{"type":"daily","time":"03:00"}`,
			Timezone: "Europe/Madrid", MisfirePolicy: "skip", PermissionProfile: "auditoria",
			TimeoutSeconds: 600, DependsOnTaskID: padre.ID, TriggerOn: trigger,
		}, "dep prompt")
		if err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	dOk := crearDep("on-success", "success")
	dFail := crearDep("on-failure", "failure")
	dAlways := crearDep("on-always", "always")

	ids := func(res string) []string {
		deps, err := db.DependientesDe(padre.ID, res)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, d := range deps {
			out = append(out, d.ID)
		}
		sort.Strings(out)
		return out
	}
	want := func(a ...string) []string { sort.Strings(a); return a }

	if got := ids("success"); !reflect.DeepEqual(got, want(dOk, dAlways)) {
		t.Errorf("success → %v, quiero %v", got, want(dOk, dAlways))
	}
	if got := ids("failure"); !reflect.DeepEqual(got, want(dFail, dAlways)) {
		t.Errorf("failure → %v, quiero %v", got, want(dFail, dAlways))
	}

	// round-trip de los campos.
	g, err := db.GetTask(dOk)
	if err != nil {
		t.Fatal(err)
	}
	if g.DependsOnTaskID != padre.ID || g.TriggerOn != "success" {
		t.Errorf("campos de dependencia no persistidos: depende=%q disparar=%q", g.DependsOnTaskID, g.TriggerOn)
	}

	// Una dependiente pausada no debe salir.
	if err := db.SetEnabled(dAlways, false); err != nil {
		t.Fatal(err)
	}
	if got := ids("success"); !reflect.DeepEqual(got, want(dOk)) {
		t.Errorf("tras pausar dAlways, success → %v, quiero %v", got, want(dOk))
	}
}
