//go:build windows

package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argalla/taskkeeper/adapters"
	"github.com/argalla/taskkeeper/packages/platform"
	"github.com/argalla/taskkeeper/packages/store"
)

// ---------- agente de mentira ----------
//
// Sustituye al CLI real para no gastar cuota en cada prueba. Emite el mismo
// flujo de eventos que Claude Code y se le puede pedir que tarde, que falle por
// credencial o que lance un nieto.

type agenteFalso struct{ guion string }

func (a *agenteFalso) Nombre() string { return "falso" }
func (a *agenteFalso) Detectar() (*adapters.Instalacion, error) {
	return &adapters.Instalacion{Ruta: "falso", Version: "0"}, nil
}
func (a *agenteFalso) Capacidades() adapters.Capacidades { return adapters.Capacidades{} }
func (a *agenteFalso) Parsear(l []byte) (adapters.Evento, bool) {
	if a.guion == "cuota" {
		return (&adapters.Codex{}).Parsear(l)
	}
	return (&adapters.Claude{}).Parsear(l)
}
func (a *agenteFalso) Comando(p adapters.Peticion) (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "-test.run=TestAyudanteAgente")
	cmd.Env = append(os.Environ(), "TK_ROL=agente", "TK_GUION="+a.guion,
		"TK_WT="+p.Worktree)
	return cmd, nil
}

// Ayudante: hace de agente. Solo actúa cuando lo invoca una prueba.
func TestAyudanteAgente(t *testing.T) {
	if os.Getenv("TK_ROL") != "agente" {
		t.Skip("no es el agente")
	}
	out := os.Stdout
	out.WriteString(`{"type":"system","subtype":"init","session_id":"sesion-falsa"}` + "\n")

	switch os.Getenv("TK_GUION") {
	case "trabaja":
		if wt := os.Getenv("TK_WT"); wt != "" {
			os.WriteFile(filepath.Join(wt, "informe.md"), []byte("# informe del agente\n"), 0o644)
		}
		out.WriteString(`{"type":"result","subtype":"success","session_id":"sesion-falsa",` +
			`"total_cost_usd":0.42,"num_turns":3,"result":"hecho"}` + "\n")

	case "credencial":
		// Como el CLI real: reintenta y no se rinde. Si el runner no corta al
		// primer 401, esta prueba se come el tiempo límite.
		for i := 1; i <= 10; i++ {
			out.WriteString(`{"type":"system","subtype":"api_retry","attempt":` +
				string(rune('0'+i)) + `,"max_retries":10,"error_status":401,` +
				`"error":"authentication_failed"}` + "\n")
			out.Sync()
			time.Sleep(2 * time.Second)
		}

	case "cuota":
		// Como Codex el 19 de agosto: texto sin codigo HTTP, con hora de reinicio.
		en1h := time.Now().Add(time.Hour)
		out.WriteString(`{"type":"error","message":"You've hit your usage limit. Upgrade or try again at ` +
			en1h.Format("Jan 2, 2006 3:04 PM") + `."}` + "\n")
		out.Sync()
		time.Sleep(20 * time.Second) // el runner debe cortar sin esperar

	case "eterno":
		// Lanza un nieto y se queda esperando, para probar la cancelación.
		nieto := exec.Command("ping", "-n", "600", "127.0.0.1")
		nieto.Start()
		out.WriteString(`{"type":"log","nieto":` + itoa(nieto.Process.Pid) + `}` + "\n")
		out.Sync()
		time.Sleep(120 * time.Second)
	}
	// Salir en seco: si dejamos terminar al arnes de pruebas, imprimiria su
	// propio "PASS" por la salida estandar y esa linea pisaria el ultimo
	// evento del agente, que es de donde sale el coste.
	out.Sync()
	os.Exit(0)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ---------- montaje ----------

func montar(t *testing.T, guion string, perfil string) (*store.DB, string, string, Opciones, Deps) {
	t.Helper()
	raiz := t.TempDir()
	repo := filepath.Join(raiz, "repo")
	os.MkdirAll(repo, 0o755)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"config", "user.name", "P"}, {"config", "user.email", "p@l"},
	} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("original\n"), 0o644)
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "inicial"}} {
		c := exec.Command("git", args...)
		c.Dir = repo
		c.CombinedOutput()
	}

	db, err := store.Open(filepath.Join(raiz, "tk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	pid, _ := db.UpsertProject(store.Project{
		Name: "demo", WorkspacePath: repo, GitRoot: repo, DefaultBranch: "main"})
	task, _, err := db.CreateTask(store.Task{
		Name: "mantenimiento", ProjectID: pid, Agent: "falso", Enabled: true,
		ConversationMode: "new", ScheduleRule: `{"type":"daily","time":"03:00"}`,
		Timezone: "Europe/Madrid", MisfirePolicy: "skip",
		PermissionProfile: perfil, TimeoutSeconds: 25,
	}, "haz el mantenimiento")
	if err != nil {
		t.Fatal(err)
	}

	o := PorDefecto()
	o.DirTurnos = filepath.Join(raiz, "turnos")
	o.DirWorktrees = filepath.Join(raiz, "worktrees")
	o.EsperaTurno = 2 * time.Second
	o.SondeoCancel = 300 * time.Millisecond

	d := Deps{
		Adaptador: func(string) (adapters.Adaptador, error) { return &agenteFalso{guion: guion}, nil },
		Grupo:     platform.NuevoGrupo,
	}
	return db, task.ID, repo, o, d
}

// Camino feliz: la ejecución termina esperando revisión, con su worktree, su
// coste y su resumen, y SIN tocar el checkout principal.
func TestEjecucionCompletaDejaTodoListoParaRevisar(t *testing.T) {
	db, taskID, repo, o, d := montar(t, "trabaja", "cambios_aislados")

	if err := Ejecutar(context.Background(), db, d, o, taskID, time.Now().UTC()); err != nil {
		t.Fatalf("Ejecutar: %v", err)
	}

	items, _ := db.Inbox()
	if len(items) != 1 {
		t.Fatalf("la bandeja tiene %d entradas, se esperaba 1", len(items))
	}
	it := items[0]
	if it.Status != store.StateAwaitingReview {
		t.Fatalf("estado = %s, se esperaba awaiting_review", it.Status)
	}
	if !it.CostUSD.Valid || it.CostUSD.Float64 != 0.42 {
		t.Errorf("no se guardó el coste informado: %+v", it.CostUSD)
	}
	if !it.Summary.Valid || !strings.Contains(it.Summary.String, "informe.md") {
		t.Errorf("el resumen no recoge los ficheros tocados: %v", it.Summary.String)
	}

	run, _ := db.GetRun(it.RunID)
	if run.WorktreePath == "" || run.BaseCommit == "" {
		t.Errorf("faltan worktree o commit base: %+v", run)
	}

	// Lo que de verdad importa.
	orig, _ := os.ReadFile(filepath.Join(repo, "README.md"))
	if string(orig) != "original\n" {
		t.Fatalf("EL CHECKOUT PRINCIPAL FUE MODIFICADO: %q", orig)
	}
	if _, err := os.Stat(filepath.Join(repo, "informe.md")); err == nil {
		t.Fatalf("el fichero del agente apareció en el checkout principal")
	}
}

// La segunda vez que llega la misma ocurrencia no se ejecuta nada.
func TestOcurrenciaRepetidaNoDuplica(t *testing.T) {
	db, taskID, _, o, d := montar(t, "trabaja", "auditoria")
	cuando := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

	for i := 0; i < 2; i++ {
		if err := Ejecutar(context.Background(), db, d, o, taskID, cuando); err != nil {
			t.Fatalf("Ejecutar #%d: %v", i+1, err)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM runs WHERE task_id=?`, taskID).Scan(&n)
	if n != 1 {
		t.Fatalf("se crearon %d ejecuciones para la misma ocurrencia", n)
	}
}

// P-06 aplicado: una credencial rechazada corta de inmediato en vez de
// consumir el tiempo límite reintentando.
func TestCredencialRechazadaCortaDeInmediato(t *testing.T) {
	db, taskID, _, o, d := montar(t, "credencial", "auditoria")

	inicio := time.Now()
	if err := Ejecutar(context.Background(), db, d, o, taskID, time.Now().UTC()); err != nil {
		t.Fatalf("Ejecutar: %v", err)
	}
	tardo := time.Since(inicio)

	items, _ := db.Inbox()
	if len(items) != 1 || items[0].Status != store.StateFailedAuth {
		t.Fatalf("estado = %v, se esperaba failed_auth", items)
	}
	// El agente falso reintenta durante 20 segundos y el timeout es de 25.
	// Si el runner esperase, tardaría al menos 20.
	if tardo > 10*time.Second {
		t.Fatalf("tardó %s: no cortó al primer 401, esperó a que el agente se rindiera", tardo)
	}
	t.Logf("cortó en %s", tardo.Round(time.Millisecond))
}

// P-24. La bandera de cancelación detiene el agente y toda su descendencia.
func TestCancelacionMataAlAgenteYSuDescendencia(t *testing.T) {
	db, taskID, _, o, d := montar(t, "eterno", "auditoria")

	hecho := make(chan struct{})
	go func() {
		defer close(hecho)
		Ejecutar(context.Background(), db, d, o, taskID, time.Now().UTC())
	}()

	// Esperar a que exista la ejecución y esté corriendo.
	var runID string
	limite := time.Now().Add(20 * time.Second)
	for time.Now().Before(limite) {
		var id, estado string
		if err := db.QueryRow(`SELECT id,status FROM runs WHERE task_id=?`, taskID).
			Scan(&id, &estado); err == nil && estado == string(store.StateRunning) {
			runID = id
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if runID == "" {
		t.Fatalf("la ejecución no llegó a estado running")
	}

	// Localizar al nieto que anunció el agente.
	var nieto int
	for time.Now().Before(limite) {
		var payload string
		if err := db.QueryRow(`SELECT payload_json FROM run_events
			WHERE run_id=? AND payload_json LIKE '%nieto%' LIMIT 1`, runID).Scan(&payload); err == nil {
			nieto = extraerPID(payload)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := db.RequestCancel(runID); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}

	select {
	case <-hecho:
	case <-time.After(30 * time.Second):
		t.Fatalf("la ejecución no se detuvo tras pedir la cancelación")
	}

	run, _ := db.GetRun(runID)
	if run.Status != store.StateCancelled {
		t.Fatalf("estado = %s, se esperaba cancelled", run.Status)
	}
	if nieto != 0 && vivo(nieto) {
		t.Fatalf("el nieto %d sobrevivió a la cancelación", nieto)
	}
	t.Logf("cancelada; nieto %d muerto", nieto)
}

func extraerPID(s string) int {
	i := strings.Index(s, `"nieto":`)
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range s[i+8:] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
