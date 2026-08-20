package runner

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/argalla/taskkeeper/packages/store"
)

// §27.4: el despacho de dependientes según el estado terminal del padre. No usa
// plataforma (sin worktree ni grupo), así que corre en cualquier SO.
func TestDispararDependientes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "tk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pid, _ := db.UpsertProject(store.Project{Name: "d", WorkspacePath: "C:\\d", GitRoot: "C:\\d", DefaultBranch: "main"})
	padre, _, err := db.CreateTask(store.Task{
		Name: "padre", ProjectID: pid, Agent: "claude", Enabled: true, ConversationMode: "new",
		ScheduleRule: `{"type":"daily","time":"03:00"}`, Timezone: "Europe/Madrid",
		MisfirePolicy: "skip", PermissionProfile: "auditoria", TimeoutSeconds: 600,
	}, "p")
	if err != nil {
		t.Fatal(err)
	}
	crearDep := func(n, tr string) string {
		task, _, err := db.CreateTask(store.Task{
			Name: n, ProjectID: pid, Agent: "claude", Enabled: true, ConversationMode: "new",
			ScheduleRule: `{"type":"daily","time":"03:00"}`, Timezone: "Europe/Madrid",
			MisfirePolicy: "skip", PermissionProfile: "auditoria", TimeoutSeconds: 600,
			DependsOnTaskID: padre.ID, TriggerOn: tr,
		}, "d")
		if err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	dOk := crearDep("ok", "success")
	dFail := crearDep("fail", "failure")
	dAlways := crearDep("always", "always")

	var lanzadas []string
	o := PorDefecto()
	o.LanzarDependiente = func(id string) error { lanzadas = append(lanzadas, id); return nil }
	run := func(st store.State) []string {
		lanzadas = nil
		dispararDependientes(db, o, padre.ID, st)
		sort.Strings(lanzadas)
		return append([]string{}, lanzadas...)
	}
	want := func(a ...string) []string { sort.Strings(a); return a }

	if got := run(store.StateCompleted); !reflect.DeepEqual(got, want(dOk, dAlways)) {
		t.Errorf("completed → %v, quiero %v (success + always)", got, want(dOk, dAlways))
	}
	if got := run(store.StateFailed); !reflect.DeepEqual(got, want(dFail, dAlways)) {
		t.Errorf("failed → %v, quiero %v (failure + always)", got, want(dFail, dAlways))
	}
	if got := run(store.StateFailedQuota); len(got) != 0 {
		t.Errorf("failed_quota → %v, quiero nada (tiene reintento programado)", got)
	}
	if got := run(store.StateAwaitingReview); len(got) != 0 {
		t.Errorf("awaiting_review → %v, quiero nada (padre aislado fuera de v1)", got)
	}
	if got := run(store.StateSkipped); len(got) != 0 {
		t.Errorf("skipped → %v, quiero nada", got)
	}
}
