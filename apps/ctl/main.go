// taskkeeper-ctl gestiona tareas. Lo invoca la extensión de VS Code; también
// sirve para trabajar sin interfaz.
//
// Todo lo que toca el programador del sistema pasa por aquí, y solo dentro de
// nuestra carpeta: ninguna tarea ajena se modifica jamás.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/argalla/taskkeeper/packages/config"
	"github.com/argalla/taskkeeper/packages/gitwt"
	"github.com/argalla/taskkeeper/packages/platform"
	"github.com/argalla/taskkeeper/packages/scheduler"
	"github.com/argalla/taskkeeper/packages/store"
)

func main() {
	if len(os.Args) < 2 {
		uso()
		os.Exit(2)
	}
	cfg := config.Cargar()
	if err := cfg.Preparar(); err != nil {
		fatal(err)
	}
	db, err := store.Open(cfg.DB)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	switch os.Args[1] {
	case "crear":
		crear(db, cfg, os.Args[2:])
	case "listar":
		listar(db)
	case "borrar":
		borrar(db, os.Args[2:])
	case "ahora":
		ahora(db, cfg, os.Args[2:])
	case "cancelar":
		cancelar(db, os.Args[2:])
	case "bandeja":
		bandeja(db)
	case "aceptar":
		decidir(db, os.Args[2:], true)
	case "rechazar":
		decidir(db, os.Args[2:], false)
	default:
		uso()
		os.Exit(2)
	}
}

func uso() {
	fmt.Fprintln(os.Stderr, `taskkeeper-ctl <orden>

  crear     --proyecto <ruta> --nombre <n> --agente claude|codex --prompt <p>
            --regla daily|weekly|once --hora HH:MM [--dias 1,5] [--zona IANA]
            [--perfil auditoria|cambios_aislados]
  listar
  borrar    <id-tarea>
  ahora     <id-tarea>          ejecuta sin alterar la próxima ocurrencia
  cancelar  <id-ejecucion>
  bandeja
  aceptar   <id-ejecucion>      funde en la rama base, en local
  rechazar  <id-ejecucion>      borra worktree y rama`)
}

func crear(db *store.DB, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("crear", flag.ExitOnError)
	proyecto := fs.String("proyecto", "", "ruta del repositorio")
	nombre := fs.String("nombre", "", "nombre de la tarea")
	agente := fs.String("agente", "claude", "claude | codex")
	prompt := fs.String("prompt", "", "instrucción para el agente")
	regla := fs.String("regla", "daily", "daily | weekly | once")
	hora := fs.String("hora", "03:00", "HH:MM")
	dias := fs.String("dias", "", "días ISO separados por coma, para weekly")
	zona := fs.String("zona", "Europe/Madrid", "zona horaria IANA")
	perfil := fs.String("perfil", "auditoria", "auditoria | cambios_aislados")
	rama := fs.String("rama", "main", "rama base")
	fs.Parse(args)

	if *proyecto == "" || *nombre == "" || *prompt == "" {
		fs.Usage()
		os.Exit(2)
	}

	ctx := context.Background()
	pf, err := gitwt.Comprobar(ctx, *proyecto, *rama)
	if err != nil {
		fatal(fmt.Errorf("el proyecto no vale: %w", err))
	}
	if pf.Sucio {
		fmt.Fprintln(os.Stderr,
			"aviso: el checkout principal tiene cambios sin guardar; no se copiarán al worktree")
	}

	projID, err := db.UpsertProject(store.Project{
		Name: nombreDeRuta(*proyecto), WorkspacePath: *proyecto,
		GitRoot: pf.GitRoot, DefaultBranch: *rama,
	})
	if err != nil {
		fatal(err)
	}

	r := scheduler.Rule{Type: scheduler.RuleType(*regla), Time: *hora}
	if *regla == "once" {
		r.AtLocal = *hora // en once, --hora lleva la fecha completa
		r.Time = ""
	}
	for _, d := range strings.Split(*dias, ",") {
		if d = strings.TrimSpace(d); d != "" {
			var n int
			fmt.Sscanf(d, "%d", &n)
			r.Weekdays = append(r.Weekdays, n)
		}
	}
	// Comprobar que la regla produce ocurrencias antes de guardarla.
	occ, err := scheduler.Next(r, *zona, time.Now().UTC())
	if err != nil {
		fatal(fmt.Errorf("la regla de calendario no vale: %w", err))
	}
	if occ == nil {
		fatal(fmt.Errorf("la regla no produce ninguna ocurrencia futura"))
	}
	if occ.AdjustedForDST {
		fmt.Fprintf(os.Stderr, "aviso: %s no existe por el cambio de hora; se ejecutará a las %s\n",
			occ.RequestedLocal, occ.ResolvedLocal)
	}

	reglaJSON, _ := json.Marshal(r)
	task, _, err := db.CreateTask(store.Task{
		Name: *nombre, ProjectID: projID, Agent: *agente, Enabled: true,
		ConversationMode: "new", ScheduleRule: string(reglaJSON), Timezone: *zona,
		MisfirePolicy: "skip", PermissionProfile: *perfil, TimeoutSeconds: 3600,
	}, *prompt)
	if err != nil {
		fatal(err)
	}

	// Registrar el disparador en el sistema operativo. A partir de aquí, quien
	// vigila el reloj es él y no nosotros.
	spec := platform.EspecDisparador{
		Tipo: *regla, Inicio: occ.ScheduledForUTC.Local(), Weekdays: r.Weekdays,
	}
	if err := platform.RegistrarTarea(task.ID, cfg.Worker, "--run "+task.ID, spec); err != nil {
		db.DeleteTask(task.ID) // no dejar una tarea sin disparador
		fatal(fmt.Errorf("registrando el disparador: %w", err))
	}
	db.SetOSTriggerID(task.ID, platform.NombreDeTarea(task.ID))

	fmt.Printf("tarea creada: %s\n", task.ID)
	fmt.Printf("próxima ejecución: %s (%s local)\n",
		occ.ScheduledForUTC.Format(time.RFC3339), occ.ResolvedLocal)
	if aviso := platform.AvisoReactivacion(); aviso != "" {
		fmt.Fprintln(os.Stderr, "aviso:", aviso)
	}
}

func listar(db *store.DB) {
	tareas, err := db.ListTasks()
	if err != nil {
		fatal(err)
	}
	if len(tareas) == 0 {
		fmt.Println("no hay tareas")
		return
	}
	for _, t := range tareas {
		estado := "activa"
		if !t.Enabled {
			estado = "pausada"
		}
		fmt.Printf("%s  %-24s %-7s %-8s %s\n", t.ID[:8], t.Name, t.Agent, estado, t.ScheduleRule)
	}
}

func borrar(db *store.DB, args []string) {
	id := arg(args, 0)
	if err := platform.RetirarTarea(id); err != nil {
		fmt.Fprintln(os.Stderr, "aviso al retirar el disparador:", err)
	}
	if err := db.DeleteTask(id); err != nil {
		fatal(err)
	}
	fmt.Println("borrada", id)
}

func ahora(db *store.DB, cfg config.Config, args []string) {
	id := arg(args, 0)
	// Instante artificial para no chocar con la clave de idempotencia de la
	// ocurrencia programada: probar a mano no debe consumir la de esta noche.
	cuando := time.Now().UTC()
	fmt.Printf("lanzando %s ...\n", id)
	if err := platform.LanzarWorker(cfg.Worker, id, cuando); err != nil {
		fatal(err)
	}
}

func cancelar(db *store.DB, args []string) {
	if err := db.RequestCancel(arg(args, 0)); err != nil {
		fatal(err)
	}
	fmt.Println("cancelación solicitada")
}

func bandeja(db *store.DB) {
	items, err := db.Inbox()
	if err != nil {
		fatal(err)
	}
	if len(items) == 0 {
		fmt.Println("nada pendiente de revisar")
		return
	}
	for _, it := range items {
		coste := ""
		if it.CostUSD.Valid {
			coste = fmt.Sprintf(" · %.2f €", it.CostUSD.Float64)
		}
		fmt.Printf("%s  %-22s %-16s %-20s%s\n",
			it.RunID[:8], it.TaskName, it.ProjectName, it.Status, coste)
		if it.Summary.Valid && it.Summary.String != "" {
			fmt.Printf("          %s\n", it.Summary.String)
		}
	}
}

func decidir(db *store.DB, args []string, aceptar bool) {
	runID := arg(args, 0)
	run, err := db.GetRun(runID)
	if err != nil {
		fatal(err)
	}
	task, err := db.GetTask(run.TaskID)
	if err != nil {
		fatal(err)
	}
	proj, err := db.GetProject(task.ProjectID)
	if err != nil {
		fatal(err)
	}
	ctx := context.Background()

	if aceptar {
		if err := gitwt.Aceptar(ctx, proj.GitRoot, run.WorktreeBranch, proj.DefaultBranch); err != nil {
			fatal(err)
		}
		db.Transition(runID, store.StateAccepted, "")
		db.Audit(runID, task.ID, "user", "accepted", `{}`)
		fmt.Println("aceptada y fundida en", proj.DefaultBranch, "(sin push)")
		return
	}
	if err := gitwt.Descartar(ctx, proj.GitRoot, run.WorktreePath, run.WorktreeBranch); err != nil {
		fatal(err)
	}
	db.Transition(runID, store.StateRejected, "")
	db.Audit(runID, task.ID, "user", "rejected", `{}`)
	fmt.Println("rechazada; worktree y rama borrados")
}

func arg(a []string, i int) string {
	if i >= len(a) {
		fmt.Fprintln(os.Stderr, "falta un argumento")
		os.Exit(2)
	}
	return a[i]
}

func nombreDeRuta(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
