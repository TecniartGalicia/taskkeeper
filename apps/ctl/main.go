// taskkeeper-ctl gestiona tareas. Lo invoca la extensión de VS Code con --json;
// también sirve para trabajar sin interfaz.
//
// Todo lo que toca el programador del sistema pasa por aquí, y solo dentro de
// nuestra carpeta: ninguna tarea ajena se modifica jamás. La extensión no abre
// la base directamente: así no necesita ningún módulo nativo y hay un solo
// sitio que sabe cómo se lee y se escribe.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/argalla/taskkeeper/adapters"
	"github.com/argalla/taskkeeper/packages/config"
	"github.com/argalla/taskkeeper/packages/gitwt"
	"github.com/argalla/taskkeeper/packages/platform"
	"github.com/argalla/taskkeeper/packages/scheduler"
	"github.com/argalla/taskkeeper/packages/store"
)

var version = "dev"

// salida encapsula texto o JSON según la bandera global.
type salida struct{ json bool }

func (s salida) ok(v any, texto func()) {
	if s.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]any{"ok": true, "data": v})
		return
	}
	texto()
}

func (s salida) fallo(err error) {
	if s.json {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": false, "error": err.Error()})
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func main() {
	args := os.Args[1:]
	out := salida{}
	// --json puede ir en cualquier posición.
	var resto []string
	for _, a := range args {
		if a == "--json" {
			out.json = true
			continue
		}
		resto = append(resto, a)
	}
	if len(resto) == 0 {
		uso()
		os.Exit(2)
	}
	if resto[0] == "version" {
		out.ok(map[string]string{"version": version}, func() { fmt.Println("taskkeeper-ctl", version) })
		return
	}
	// previsualizar es cálculo puro (no toca la base): lo llama el panel a menudo.
	if resto[0] == "previsualizar" {
		previsualizar(out, resto[1:])
		return
	}

	cfg := config.Cargar()
	if err := cfg.Preparar(); err != nil {
		out.fallo(err)
	}
	db, err := store.Open(cfg.DB)
	if err != nil {
		out.fallo(err)
	}
	defer db.Close()
	// Cualquier escritura desde aquí también avisa a la extensión.
	db.OnChange = func() { config.TocarMarca(cfg) }

	switch resto[0] {
	case "crear":
		crear(out, db, cfg, resto[1:])
	case "editar":
		editar(out, db, cfg, resto[1:])
	case "listar":
		listar(out, db)
	case "tarea":
		tarea(out, db, resto[1:])
	case "borrar":
		borrar(out, db, resto[1:])
	case "pausar":
		activar(out, db, cfg, resto[1:], false)
	case "reanudar":
		activar(out, db, cfg, resto[1:], true)
	case "ahora":
		ahora(out, db, cfg, resto[1:])
	case "cancelar":
		cancelar(out, db, resto[1:])
	case "cupo":
		cupo(out, db, resto[1:])
	case "bandeja":
		bandeja(out, db)
	case "historial":
		historial(out, db, resto[1:])
	case "ejecucion":
		ejecucion(out, db, resto[1:])
	case "eventos":
		eventos(out, db, resto[1:])
	case "aceptar":
		decidir(out, db, resto[1:], true)
	case "rechazar":
		decidir(out, db, resto[1:], false)
	case "archivar":
		archivar(out, db, resto[1:])
	case "agentes":
		agentes(out)
	case "entorno":
		entorno(out, cfg)
	case "resumen":
		resumen(out, db, resto[1:])
	default:
		uso()
		os.Exit(2)
	}
}

// resumen: «lo que pasó anoche» (ventana móvil --horas, 16 por defecto) + salud
// del planificador por tarea. La comprobación honesta es `platform.TareaExiste`:
// un disparador del SO que se desregistró se marca aquí (registrada=false).
func resumen(out salida, db *store.DB, args []string) {
	fs := flag.NewFlagSet("resumen", flag.ExitOnError)
	horas := fs.Int("horas", 16, "ventana de «anoche» en horas hacia atrás")
	fs.Parse(args)
	ahora := time.Now().UTC()
	res, err := db.ResumenNoche(ahora.Add(-time.Duration(*horas) * time.Hour).Format(time.RFC3339))
	if err != nil {
		out.fallo(err)
	}
	base, err := db.SaludBase(ahora.AddDate(0, 0, -7).Format(time.RFC3339))
	if err != nil {
		out.fallo(err)
	}
	type saludJSON struct {
		TareaID          string `json:"tarea_id"`
		Tarea            string `json:"tarea"`
		Registrada       bool   `json:"registrada"`
		ProximaLocal     string `json:"proxima_local"`
		DisparosPerdidos int    `json:"disparos_perdidos"`
		Pendientes       int    `json:"pendientes"`
	}
	salud := []saludJSON{}
	for _, b := range base {
		reg := platform.TareaExiste(b.TareaID) // id CRUDO, no os_trigger_id (se doble-envolvería)
		prox := ""
		// Sin disparador registrado la «próxima» sería una hora falsa que nunca
		// llega, así que solo se calcula cuando de verdad está registrada.
		if reg {
			if t, err := db.GetTask(b.TareaID); err == nil && t.Enabled {
				var r scheduler.Rule
				if json.Unmarshal([]byte(t.ScheduleRule), &r) == nil {
					if occ, err := scheduler.Next(r, t.Timezone, ahora); err == nil && occ != nil {
						prox = occ.ResolvedLocal
					}
				}
			}
		}
		salud = append(salud, saludJSON{b.TareaID, b.Tarea, reg, prox, b.DisparosPerdidos, b.Pendientes})
	}
	out.ok(map[string]any{"resumen": res, "salud": salud}, func() {
		fmt.Printf("anoche: %d terminadas · %d esperan · %d fallidas · %d saltadas · %d en curso · %.2f USD\n",
			res.Terminadas, res.EsperanRevision, res.Fallidas, res.Saltadas, res.EnCurso, res.CosteTotalUSD)
		for _, s := range salud {
			estado := "ok"
			if !s.Registrada {
				estado = "SIN DISPARADOR"
			}
			fmt.Printf("  %-24s %-16s perdidos:%d pendientes:%d\n", s.Tarea, estado, s.DisparosPerdidos, s.Pendientes)
		}
	})
}

func uso() {
	fmt.Fprintln(os.Stderr, `taskkeeper-ctl [--json] <orden>

  crear     --proyecto <ruta> --nombre <n> --agente claude|codex --prompt <p>
            --regla daily|weekly|once --hora HH:MM [--dias 1,5] [--zona IANA]
            [--perfil auditoria|cambios_aislados] [--modo new|resume|fork]
            [--sesion <id>] [--politica skip|run_if_late|manual] [--retraso-max <seg>]
            [--timeout <seg>] [--presupuesto <usd>] [--presupuesto-diario <usd>]
  editar    <id-tarea> [las mismas opciones que crear]
  listar
  tarea     <id-tarea>
  pausar    <id-tarea>  ·  reanudar <id-tarea>
  borrar    <id-tarea>
  ahora     <id-tarea>          ejecuta en segundo plano sin alterar la próxima ocurrencia
  cancelar  <id-ejecucion>
  cupo      [n]                 ejecuciones simultáneas en esta máquina
  bandeja                       lo que espera decisión
  historial [--limite n]
  ejecucion <id-ejecucion>      detalle: worktree, commit base, ficheros
  eventos   <id-ejecucion> [--desde <seq>]
  aceptar   <id-ejecucion>      funde en la rama base, en local
  rechazar  <id-ejecucion>      borra worktree y rama
  archivar  <id-ejecucion>      saca de la bandeja una ejecucion fallida o sin cambios; limpia su worktree
  agentes                       qué agentes hay instalados y dónde
  entorno                       rutas y avisos de esta máquina
  resumen   [--horas n]         resumen de anoche + salud del planificador
  version`)
}

// ---------- alta y edición ----------

type opcionesTarea struct {
	proyecto, nombre, agente, prompt, regla, hora, dias, zona, perfil, rama string
	modo, sesion, politica, autocompact, workspace                          string
	retrasoMax, timeout                                                     int
	presupuesto, presupuestoDiario                                          float64
}

func parsearOpciones(nombre string, args []string, base *store.Task, promptBase string) (opcionesTarea, *flag.FlagSet) {
	fs := flag.NewFlagSet(nombre, flag.ExitOnError)
	var o opcionesTarea
	def := func(deTarea, d string) string {
		if base != nil && deTarea != "" {
			return deTarea
		}
		return d
	}
	fs.StringVar(&o.proyecto, "proyecto", "", "ruta del repositorio")
	fs.StringVar(&o.nombre, "nombre", def(nombreDe(base), ""), "nombre de la tarea")
	fs.StringVar(&o.agente, "agente", def(agenteDe(base), "claude"), "claude | codex")
	fs.StringVar(&o.prompt, "prompt", promptBase, "instrucción para el agente")
	// Al editar, la regla existente es el valor por defecto: pasar solo --hora no
	// debe convertir una tarea semanal en diaria.
	dRegla, dHora, dDias := "daily", "03:00", ""
	if base != nil {
		var r scheduler.Rule
		if json.Unmarshal([]byte(base.ScheduleRule), &r) == nil && r.Type != "" {
			dRegla = string(r.Type)
			if r.Type == scheduler.RuleOnce {
				dHora = r.AtLocal
			} else if hs := scheduler.TimesList(r); len(hs) > 0 {
				dHora = strings.Join(hs, ",")
			}
			var ds []string
			for _, d := range r.Weekdays {
				ds = append(ds, strconv.Itoa(d))
			}
			dDias = strings.Join(ds, ",")
		}
	}
	fs.StringVar(&o.regla, "regla", dRegla, "daily | weekly | once")
	fs.StringVar(&o.hora, "hora", dHora, "HH:MM, o fecha completa YYYY-MM-DDTHH:MM en once")
	fs.StringVar(&o.dias, "dias", dDias, "días ISO separados por coma, para weekly")
	fs.StringVar(&o.zona, "zona", def(zonaDe(base), "Europe/Madrid"), "zona horaria IANA")
	fs.StringVar(&o.perfil, "perfil", def(perfilDe(base), "auditoria"), "auditoria | cambios_aislados")
	fs.StringVar(&o.rama, "rama", "main", "rama base")
	fs.StringVar(&o.modo, "modo", def(modoDe(base), "new"), "new | resume | fork")
	fs.StringVar(&o.sesion, "sesion", "", "identificador de sesión para resume o fork")
	fs.StringVar(&o.politica, "politica", def(politicaDe(base), "skip"), "skip | run_if_late | manual")
	dAutocompact := ""
	if base != nil {
		dAutocompact = base.Autocompact
	}
	fs.StringVar(&o.autocompact, "autocompact", dAutocompact, "ventana de autocompactación de Claude Code (auto | tokens); vacío = por defecto")
	dWorkspace := "isolated"
	if base != nil && base.WorkspaceMode != "" {
		dWorkspace = base.WorkspaceMode
	}
	fs.StringVar(&o.workspace, "workspace", dWorkspace, "isolated (worktree + revisión) | direct (en el repo real, sin revisión)")
	dRetraso, dTimeout, dPres, dPresDia := 7200, 3600, 0.0, 0.0
	if base != nil {
		dRetraso, dTimeout = base.MaxLatenessSeconds, base.TimeoutSeconds
		if base.MaxBudgetUSD.Valid {
			dPres = base.MaxBudgetUSD.Float64
		}
		if base.DailyBudgetUSD.Valid {
			dPresDia = base.DailyBudgetUSD.Float64
		}
	}
	fs.IntVar(&o.retrasoMax, "retraso-max", dRetraso, "segundos de retraso admitidos con run_if_late")
	fs.IntVar(&o.timeout, "timeout", dTimeout, "segundos máximos por ejecución")
	fs.Float64Var(&o.presupuesto, "presupuesto", dPres, "USD máximos por ejecución (0 = sin tope)")
	fs.Float64Var(&o.presupuestoDiario, "presupuesto-diario", dPresDia, "USD máximos al día para esta tarea")
	fs.Parse(args)
	return o, fs
}

func crear(out salida, db *store.DB, cfg config.Config, args []string) {
	o, fs := parsearOpciones("crear", args, nil, "")
	if o.proyecto == "" || o.nombre == "" || o.prompt == "" {
		if out.json {
			out.fallo(fmt.Errorf("faltan --proyecto, --nombre o --prompt"))
		}
		fs.Usage()
		os.Exit(2)
	}
	if (o.modo == "resume" || o.modo == "fork") && o.sesion == "" {
		out.fallo(fmt.Errorf("el modo %s necesita --sesion", o.modo))
	}
	if o.workspace != "isolated" && o.workspace != "direct" {
		out.fallo(fmt.Errorf("--workspace debe ser isolated o direct"))
	}

	ctx := context.Background()
	// En modo directo («en la conversación») basta con que la carpeta exista: no
	// se crea worktree, así que no se exige repositorio Git ni rama base. En modo
	// aislado sí se comprueba el repo y el commit de la rama.
	var pf *gitwt.Preflight
	var err error
	if o.workspace == "direct" {
		pf, err = gitwt.ComprobarSuave(ctx, o.proyecto)
	} else {
		pf, err = gitwt.Comprobar(ctx, o.proyecto, o.rama)
	}
	if err != nil {
		out.fallo(fmt.Errorf("el proyecto no vale: %w", err))
	}
	projID, err := db.UpsertProject(store.Project{
		Name: nombreDeRuta(o.proyecto), WorkspacePath: o.proyecto,
		GitRoot: pf.GitRoot, DefaultBranch: o.rama,
	})
	if err != nil {
		out.fallo(err)
	}

	regla, occ, avisos, err := construirRegla(o)
	if err != nil {
		out.fallo(err)
	}
	if pf.Sucio {
		avisos = append(avisos, "el checkout principal tiene cambios sin guardar; no se copiarán al worktree")
	}

	var sesion store.NullString
	if o.sesion != "" {
		id, err := db.UpsertSessionRef(o.agente, o.sesion, o.proyecto)
		if err != nil {
			out.fallo(err)
		}
		sesion = store.NullString{String: id, Valid: true}
	}

	reglaJSON, _ := json.Marshal(regla)
	task, _, err := db.CreateTask(store.Task{
		Name: o.nombre, ProjectID: projID, Agent: o.agente, Enabled: true,
		ConversationMode: o.modo, SessionRefID: sesion,
		ScheduleRule: string(reglaJSON), Timezone: o.zona,
		MisfirePolicy: o.politica, MaxLatenessSeconds: o.retrasoMax,
		PermissionProfile: o.perfil, TimeoutSeconds: o.timeout,
		MaxBudgetUSD:   store.NullFloat(o.presupuesto),
		DailyBudgetUSD: store.NullFloat(o.presupuestoDiario),
		Autocompact:    o.autocompact,
		WorkspaceMode:  o.workspace,
	}, o.prompt)
	if err != nil {
		out.fallo(err)
	}

	if err := registrar(cfg, task.ID, o.zona, regla, occ); err != nil {
		db.DeleteTask(task.ID) // no dejar una tarea sin disparador
		out.fallo(fmt.Errorf("registrando el disparador: %w", err))
	}
	db.SetOSTriggerID(task.ID, platform.NombreDeTarea(task.ID))
	if a := platform.AvisoReactivacion(); a != "" {
		avisos = append(avisos, a)
	}
	if avisos == nil {
		avisos = []string{}
	}

	res := map[string]any{
		"id": task.ID, "next_run_utc": occ.ScheduledForUTC.Format(time.RFC3339),
		"next_run_local": occ.ResolvedLocal, "avisos": avisos,
	}
	out.ok(res, func() {
		fmt.Printf("tarea creada: %s\npróxima ejecución: %s (%s local)\n",
			task.ID, occ.ScheduledForUTC.Format(time.RFC3339), occ.ResolvedLocal)
		for _, a := range avisos {
			fmt.Fprintln(os.Stderr, "aviso:", a)
		}
	})
}

func editar(out salida, db *store.DB, cfg config.Config, args []string) {
	if len(args) == 0 {
		out.fallo(fmt.Errorf("falta el identificador de la tarea"))
	}
	id := args[0]
	base, err := db.GetTask(id)
	if err != nil {
		out.fallo(fmt.Errorf("tarea %s: %w", id, err))
	}
	rev, err := db.LatestRevision(id)
	if err != nil {
		out.fallo(err)
	}
	o, _ := parsearOpciones("editar", args[1:], base, rev.Prompt)
	if o.workspace != "isolated" && o.workspace != "direct" {
		out.fallo(fmt.Errorf("--workspace debe ser isolated o direct"))
	}
	// Pasar a modo aislado exige un repositorio Git válido: se comprueba AQUÍ para
	// fallar al editar y no a la hora de ejecutar (una tarea directa pudo crearse
	// sobre una carpeta que no es git).
	if o.workspace == "isolated" {
		if p, err := db.GetProject(base.ProjectID); err == nil {
			if _, err := gitwt.Comprobar(context.Background(), p.WorkspacePath, o.rama); err != nil {
				out.fallo(fmt.Errorf("el modo aislado necesita un repositorio Git: %w", err))
			}
		}
	}

	regla, occ, avisos, err := construirRegla(o)
	if err != nil {
		out.fallo(err)
	}
	reglaJSON, _ := json.Marshal(regla)
	nueva := *base
	nueva.Name, nueva.Agent = o.nombre, o.agente
	nueva.ScheduleRule, nueva.Timezone = string(reglaJSON), o.zona
	nueva.PermissionProfile, nueva.MisfirePolicy = o.perfil, o.politica
	nueva.MaxLatenessSeconds, nueva.TimeoutSeconds = o.retrasoMax, o.timeout
	nueva.ConversationMode = o.modo
	nueva.MaxBudgetUSD = store.NullFloat(o.presupuesto)
	nueva.DailyBudgetUSD = store.NullFloat(o.presupuestoDiario)
	nueva.Autocompact = o.autocompact
	nueva.WorkspaceMode = o.workspace
	if o.sesion != "" {
		sid, err := db.UpsertSessionRef(o.agente, o.sesion, base.ProjectID)
		if err != nil {
			out.fallo(err)
		}
		nueva.SessionRefID = store.NullString{String: sid, Valid: true}
	}

	if err := db.UpdateTask(nueva, o.prompt); err != nil {
		out.fallo(err)
	}
	if base.Enabled {
		if err := registrar(cfg, id, o.zona, regla, occ); err != nil {
			out.fallo(fmt.Errorf("actualizando el disparador: %w", err))
		}
	}
	if avisos == nil {
		avisos = []string{}
	}
	out.ok(map[string]any{"id": id, "next_run_utc": occ.ScheduledForUTC.Format(time.RFC3339),
		"next_run_local": occ.ResolvedLocal, "avisos": avisos},
		func() { fmt.Printf("tarea %s actualizada; próxima ejecución %s\n", id, occ.ResolvedLocal) })
}

// previsualizar devuelve las próximas ocurrencias de una regla, sin crear nada.
// La matemática de fechas vive solo aquí (Go); el panel nunca la reimplementa.
func previsualizar(out salida, args []string) {
	fs := flag.NewFlagSet("previsualizar", flag.ExitOnError)
	var regla, hora, dias, zona string
	fs.StringVar(&regla, "regla", "daily", "daily | weekly | once")
	fs.StringVar(&hora, "hora", "", "HH:MM (varias con coma), o fecha completa en once")
	fs.StringVar(&dias, "dias", "", "días ISO separados por coma, para weekly")
	fs.StringVar(&zona, "zona", "Europe/Madrid", "zona horaria IANA")
	fs.Parse(args)

	r := scheduler.Rule{Type: scheduler.RuleType(regla)}
	if regla == "once" {
		r.AtLocal = hora
	} else {
		for _, h := range strings.Split(hora, ",") {
			if h = strings.TrimSpace(h); h != "" {
				r.Times = append(r.Times, h)
			}
		}
	}
	for _, d := range strings.Split(dias, ",") {
		if d = strings.TrimSpace(d); d != "" {
			if n, err := strconv.Atoi(d); err == nil {
				r.Weekdays = append(r.Weekdays, n)
			}
		}
	}

	loc, _ := time.LoadLocation(zona)
	next := []string{}
	after := time.Now().UTC()
	for i := 0; i < 3; i++ {
		occ, err := scheduler.Next(r, zona, after)
		if err != nil {
			out.fallo(err)
		}
		if occ == nil {
			break
		}
		tt := occ.ScheduledForUTC
		if loc != nil {
			tt = tt.In(loc)
		}
		next = append(next, tt.Format("Mon 02 Jan · 15:04"))
		after = occ.ScheduledForUTC
	}
	out.ok(map[string]any{"next": next}, func() {
		for _, n := range next {
			fmt.Println(n)
		}
	})
}

func construirRegla(o opcionesTarea) (scheduler.Rule, *scheduler.Occurrence, []string, error) {
	r := scheduler.Rule{Type: scheduler.RuleType(o.regla)}
	if o.regla == "once" {
		r.AtLocal = o.hora
	} else {
		// --hora admite varias separadas por coma: "15:00,20:00".
		for _, h := range strings.Split(o.hora, ",") {
			if h = strings.TrimSpace(h); h != "" {
				r.Times = append(r.Times, h)
			}
		}
	}
	for _, d := range strings.Split(o.dias, ",") {
		if d = strings.TrimSpace(d); d != "" {
			n, err := strconv.Atoi(d)
			if err != nil {
				return r, nil, nil, fmt.Errorf("día %q no es un número", d)
			}
			r.Weekdays = append(r.Weekdays, n)
		}
	}
	occ, err := scheduler.Next(r, o.zona, time.Now().UTC())
	if err != nil {
		return r, nil, nil, fmt.Errorf("la regla de calendario no vale: %w", err)
	}
	if occ == nil {
		return r, nil, nil, fmt.Errorf("la regla no produce ninguna ocurrencia futura")
	}
	var avisos []string
	if occ.AdjustedForDST {
		avisos = append(avisos, fmt.Sprintf("%s no existe por el cambio de hora; se ejecutará a las %s",
			occ.RequestedLocal, occ.ResolvedLocal))
	}
	return r, occ, avisos, nil
}

func registrar(cfg config.Config, taskID, zona string, r scheduler.Rule, occ *scheduler.Occurrence) error {
	// Un StartBoundary por cada hora del día: así "todos los días a las 15:00 y
	// 20:00" se convierte en dos disparadores del sistema.
	var horas []time.Time
	if r.Type != scheduler.RuleOnce {
		now := time.Now().UTC()
		for _, h := range scheduler.TimesList(r) {
			sub := scheduler.Rule{Type: r.Type, Time: h, Weekdays: r.Weekdays}
			if o, err := scheduler.Next(sub, zona, now); err == nil && o != nil {
				horas = append(horas, o.ScheduledForUTC.Local())
			}
		}
	}
	if len(horas) == 0 {
		horas = []time.Time{occ.ScheduledForUTC.Local()}
	}
	spec := platform.EspecDisparador{
		Tipo: string(r.Type), Inicio: horas[0], Horas: horas, Weekdays: r.Weekdays,
	}
	return platform.RegistrarTarea(taskID, cfg.Worker, "--run "+taskID, spec)
}

// ---------- consulta ----------

type tareaJSON struct {
	ID                   string   `json:"id"`
	Nombre               string   `json:"nombre"`
	Proyecto             string   `json:"proyecto"`
	RutaProyecto         string   `json:"ruta_proyecto"`
	Agente               string   `json:"agente"`
	Activa               bool     `json:"activa"`
	Modo                 string   `json:"modo"`
	SesionExterna        string   `json:"sesion_externa"`
	Autocompact          string   `json:"autocompact"`
	WorkspaceMode        string   `json:"workspace_mode"`
	Regla                string   `json:"regla"`
	Zona                 string   `json:"zona"`
	Perfil               string   `json:"perfil"`
	Politica             string   `json:"politica"`
	TimeoutSeg           int      `json:"timeout_seg"`
	RetrasoMaxSeg        int      `json:"retraso_max_seg"`
	PresupuestoUSD       *float64 `json:"presupuesto_usd"`
	PresupuestoDiarioUSD *float64 `json:"presupuesto_diario_usd"`
	ProximaUTC           string   `json:"proxima_utc"`
	ProximaLocal         string   `json:"proxima_local"`
	Prompt               string   `json:"prompt"`
	UltimoEstado         string   `json:"ultimo_estado"`
	UltimaEjecucion      string   `json:"ultima_ejecucion"`
}

func aJSON(db *store.DB, t *store.Task) tareaJSON {
	j := tareaJSON{
		ID: t.ID, Nombre: t.Name, Agente: t.Agent, Activa: t.Enabled,
		Modo: t.ConversationMode, Regla: t.ScheduleRule, Zona: t.Timezone,
		Perfil: t.PermissionProfile, Politica: t.MisfirePolicy, Autocompact: t.Autocompact,
		WorkspaceMode: defaultStr2(t.WorkspaceMode),
		TimeoutSeg:    t.TimeoutSeconds, RetrasoMaxSeg: t.MaxLatenessSeconds,
	}
	if p, err := db.GetProject(t.ProjectID); err == nil {
		j.Proyecto, j.RutaProyecto = p.Name, p.WorkspacePath
	}
	if t.SessionRefID.Valid && t.SessionRefID.String != "" {
		if ref, err := db.GetSessionRef(t.SessionRefID.String); err == nil && ref != nil {
			j.SesionExterna = ref.ExternalID
		}
	}
	if t.MaxBudgetUSD.Valid {
		v := t.MaxBudgetUSD.Float64
		j.PresupuestoUSD = &v
	}
	if t.DailyBudgetUSD.Valid {
		v := t.DailyBudgetUSD.Float64
		j.PresupuestoDiarioUSD = &v
	}
	var r scheduler.Rule
	if json.Unmarshal([]byte(t.ScheduleRule), &r) == nil && t.Enabled {
		if occ, err := scheduler.Next(r, t.Timezone, time.Now().UTC()); err == nil && occ != nil {
			j.ProximaUTC, j.ProximaLocal = occ.ScheduledForUTC.Format(time.RFC3339), occ.ResolvedLocal
		}
	}
	if rev, err := db.LatestRevision(t.ID); err == nil {
		j.Prompt = rev.Prompt
	}
	if est, cuando, err := db.LastRunState(t.ID); err == nil {
		j.UltimoEstado, j.UltimaEjecucion = est, cuando
	}
	return j
}

func listar(out salida, db *store.DB) {
	tareas, err := db.ListTasks()
	if err != nil {
		out.fallo(err)
	}
	lista := []tareaJSON{}
	for _, t := range tareas {
		lista = append(lista, aJSON(db, t))
	}
	out.ok(lista, func() {
		if len(lista) == 0 {
			fmt.Println("no hay tareas")
			return
		}
		for _, t := range lista {
			estado := "activa"
			if !t.Activa {
				estado = "pausada"
			}
			fmt.Printf("%s  %-24s %-7s %-8s próx. %s\n", t.ID[:8], t.Nombre, t.Agente, estado, t.ProximaLocal)
		}
	})
}

func tarea(out salida, db *store.DB, args []string) {
	t, err := db.GetTask(arg(out, args, 0))
	if err != nil {
		out.fallo(err)
	}
	j := aJSON(db, t)
	out.ok(j, func() {
		b, _ := json.MarshalIndent(j, "", "  ")
		fmt.Println(string(b))
	})
}

func borrar(out salida, db *store.DB, args []string) {
	id := arg(out, args, 0)
	if err := platform.RetirarTarea(id); err != nil && !out.json {
		fmt.Fprintln(os.Stderr, "aviso al retirar el disparador:", err)
	}
	if err := db.DeleteTask(id); err != nil {
		out.fallo(err)
	}
	out.ok(map[string]string{"id": id}, func() { fmt.Println("borrada", id) })
}

func activar(out salida, db *store.DB, cfg config.Config, args []string, activa bool) {
	id := arg(out, args, 0)
	t, err := db.GetTask(id)
	if err != nil {
		out.fallo(err)
	}
	// El disparador acompaña al estado: pausada = sin disparador. Así una tarea
	// pausada no despierta el equipo para nada.
	if activa {
		var r scheduler.Rule
		json.Unmarshal([]byte(t.ScheduleRule), &r)
		occ, err := scheduler.Next(r, t.Timezone, time.Now().UTC())
		if err != nil || occ == nil {
			out.fallo(fmt.Errorf("la regla ya no produce ocurrencias; edita la tarea"))
		}
		if err := registrar(cfg, id, t.Timezone, r, occ); err != nil {
			out.fallo(err)
		}
	} else {
		platform.RetirarTarea(id)
	}
	if err := db.SetEnabled(id, activa); err != nil {
		out.fallo(err)
	}
	out.ok(map[string]any{"id": id, "activa": activa}, func() {
		if activa {
			fmt.Println("reanudada", id)
		} else {
			fmt.Println("pausada", id)
		}
	})
}

func ahora(out salida, db *store.DB, cfg config.Config, args []string) {
	id := arg(out, args, 0)
	if _, err := db.GetTask(id); err != nil {
		out.fallo(err)
	}
	// Instante actual, no la hora prevista: probar a mano no debe consumir la
	// ocurrencia programada de esta noche.
	cuando := time.Now().UTC()
	if err := platform.LanzarWorker(cfg.Worker, id, cuando); err != nil {
		out.fallo(err)
	}
	out.ok(map[string]any{"id": id, "lanzada": true}, func() {
		fmt.Printf("lanzada %s en segundo plano; sigue el progreso con `bandeja`\n", id)
	})
}

func cancelar(out salida, db *store.DB, args []string) {
	id := arg(out, args, 0)
	if err := db.RequestCancel(id); err != nil {
		out.fallo(err)
	}
	out.ok(map[string]any{"id": id}, func() { fmt.Println("cancelación solicitada") })
}

func cupo(out salida, db *store.DB, args []string) {
	if len(args) == 0 {
		n := db.Cupo()
		out.ok(map[string]int{"cupo": n}, func() { fmt.Printf("cupo actual: %d\n", n) })
		return
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		out.fallo(fmt.Errorf("el cupo debe ser un número mayor que cero"))
	}
	if err := db.FijarAjuste(store.ClaveCupo, args[0]); err != nil {
		out.fallo(err)
	}
	out.ok(map[string]int{"cupo": n}, func() { fmt.Printf("cupo fijado en %d\n", n) })
}

type ejecucionJSON struct {
	ID              string   `json:"id"`
	TareaID         string   `json:"tarea_id"`
	Tarea           string   `json:"tarea"`
	Proyecto        string   `json:"proyecto"`
	RutaProyecto    string   `json:"ruta_proyecto"`
	RaizGit         string   `json:"raiz_git"`
	RamaBase        string   `json:"rama_base"`
	Estado          string   `json:"estado"`
	PrevistaUTC     string   `json:"prevista_utc"`
	Inicio          string   `json:"inicio"`
	Fin             string   `json:"fin"`
	Worktree        string   `json:"worktree"`
	Rama            string   `json:"rama"`
	CommitBase      string   `json:"commit_base"`
	CosteUSD        *float64 `json:"coste_usd"`
	Resumen         string   `json:"resumen"`
	CodigoError     string   `json:"codigo_error"`
	SesionProveedor string   `json:"sesion_proveedor"`
	Ficheros        []string `json:"ficheros"`
	Decision        string   `json:"decision"`
	WorkspaceMode   string   `json:"workspace_mode"`
	Cancelando      bool     `json:"cancelando"`
	Agente          string   `json:"agente"`
}

func aEjecucionJSON(r *store.RunDetalle) ejecucionJSON {
	j := ejecucionJSON{
		ID: r.ID, TareaID: r.TaskID, Tarea: r.TaskName, Proyecto: r.ProjectName,
		RutaProyecto: r.WorkspacePath, RaizGit: r.GitRoot, RamaBase: r.DefaultBranch,
		Estado: string(r.Status), PrevistaUTC: r.ScheduledForUTC,
		Inicio: r.StartedAt, Fin: r.FinishedAt,
		Worktree: r.WorktreePath, Rama: r.WorktreeBranch, CommitBase: r.BaseCommit,
		Resumen: r.Summary, CodigoError: r.ErrorCode, SesionProveedor: r.ProviderSession,
		Decision: r.ReviewDecision, WorkspaceMode: r.WorkspaceMode, Cancelando: r.CancelRequested, Agente: r.Agent,
		Ficheros: []string{},
	}
	if r.CostUSD.Valid {
		v := r.CostUSD.Float64
		j.CosteUSD = &v
	}
	var res struct {
		Ficheros []string `json:"ficheros"`
	}
	if json.Unmarshal([]byte(r.Summary), &res) == nil && res.Ficheros != nil {
		j.Ficheros = res.Ficheros
	}
	return j
}

func bandeja(out salida, db *store.DB) {
	items, err := db.InboxDetalle()
	if err != nil {
		out.fallo(err)
	}
	lista := []ejecucionJSON{}
	for i := range items {
		lista = append(lista, aEjecucionJSON(&items[i]))
	}
	out.ok(lista, func() {
		if len(lista) == 0 {
			fmt.Println("nada pendiente de revisar")
			return
		}
		for _, it := range lista {
			coste := ""
			if it.CosteUSD != nil {
				coste = fmt.Sprintf(" · %.2f USD", *it.CosteUSD)
			}
			fmt.Printf("%s  %-22s %-16s %-20s%s\n", it.ID[:8], it.Tarea, it.Proyecto, it.Estado, coste)
		}
	})
}

func historial(out salida, db *store.DB, args []string) {
	fs := flag.NewFlagSet("historial", flag.ExitOnError)
	limite := fs.Int("limite", 50, "máximo de ejecuciones")
	fs.Parse(args)
	items, err := db.Historial(*limite)
	if err != nil {
		out.fallo(err)
	}
	lista := []ejecucionJSON{}
	for i := range items {
		lista = append(lista, aEjecucionJSON(&items[i]))
	}
	out.ok(lista, func() {
		for _, it := range lista {
			fmt.Printf("%s  %-22s %-20s %s\n", it.ID[:8], it.Tarea, it.Estado, it.Fin)
		}
	})
}

func ejecucion(out salida, db *store.DB, args []string) {
	r, err := db.GetRunDetalle(arg(out, args, 0))
	if err != nil {
		out.fallo(err)
	}
	j := aEjecucionJSON(r)
	out.ok(j, func() {
		b, _ := json.MarshalIndent(j, "", "  ")
		fmt.Println(string(b))
	})
}

func eventos(out salida, db *store.DB, args []string) {
	id := arg(out, args, 0)
	fs := flag.NewFlagSet("eventos", flag.ExitOnError)
	desde := fs.Int("desde", 0, "secuencia a partir de la cual devolver")
	fs.Parse(args[1:])
	evs, err := db.Eventos(id, *desde)
	if err != nil {
		out.fallo(err)
	}
	if evs == nil {
		evs = []store.Evento{}
	}
	out.ok(evs, func() {
		for _, e := range evs {
			fmt.Printf("%4d %-16s %s\n", e.Sequence, e.Type, e.Payload)
		}
	})
}

func decidir(out salida, db *store.DB, args []string, aceptar bool) {
	runID := arg(out, args, 0)
	r, err := db.GetRunDetalle(runID)
	if err != nil {
		out.fallo(err)
	}
	if r.Status != store.StateAwaitingReview {
		out.fallo(fmt.Errorf("la ejecución está en %s; solo se decide sobre awaiting_review", r.Status))
	}
	ctx := context.Background()
	if aceptar {
		if err := gitwt.Aceptar(ctx, r.GitRoot, r.WorktreeBranch, r.DefaultBranch); err != nil {
			out.fallo(err)
		}
		// Tras fundir, el worktree ya no hace falta.
		gitwt.Descartar(ctx, r.GitRoot, r.WorktreePath, r.WorktreeBranch)
		db.Transition(runID, store.StateAccepted, "")
		db.SetReview(runID, "accepted")
		db.Audit(runID, r.TaskID, "user", "accepted", `{}`)
		out.ok(map[string]any{"id": runID, "decision": "accepted"}, func() {
			fmt.Println("aceptada y fundida en", r.DefaultBranch, "(sin push)")
		})
		return
	}
	if err := gitwt.Descartar(ctx, r.GitRoot, r.WorktreePath, r.WorktreeBranch); err != nil {
		out.fallo(err)
	}
	db.Transition(runID, store.StateRejected, "")
	db.SetReview(runID, "rejected")
	db.Audit(runID, r.TaskID, "user", "rejected", `{}`)
	out.ok(map[string]any{"id": runID, "decision": "rejected"}, func() {
		fmt.Println("rechazada; worktree y rama borrados")
	})
}

// archivar es la salida de la bandeja para lo que no se acepta ni se rechaza:
// una ejecución fallida, o una auditoría que terminó bien sin tocar ficheros.
// Limpia el worktree y la rama, marca la decisión y no toca la rama base.
func archivar(out salida, db *store.DB, args []string) {
	runID := arg(out, args, 0)
	r, err := db.GetRunDetalle(runID)
	if err != nil {
		out.fallo(err)
	}
	if store.EsActivo(r.Status) {
		out.fallo(fmt.Errorf("la ejecución sigue en curso (%s); cancélala antes de archivarla", r.Status))
	}
	if r.Status == store.StateAwaitingReview && len(aEjecucionJSON(r).Ficheros) > 0 {
		out.fallo(fmt.Errorf("esta ejecución tiene cambios pendientes: acéptala o recházala"))
	}
	ctx := context.Background()
	if r.WorktreePath != "" {
		// Un fallo puede haber dejado el worktree a medias; se limpia lo que haya.
		gitwt.Descartar(ctx, r.GitRoot, r.WorktreePath, r.WorktreeBranch)
	}
	if r.Status == store.StateAwaitingReview {
		db.Transition(runID, store.StateAccepted, "sin cambios que fundir")
	}
	db.SetReview(runID, "archived")
	db.Audit(runID, r.TaskID, "user", "archived", `{}`)
	out.ok(map[string]any{"id": runID, "decision": "archived"}, func() {
		fmt.Println("archivada; worktree limpiado")
	})
}

func agentes(out salida) {
	type ag struct {
		Nombre    string `json:"nombre"`
		Ruta      string `json:"ruta"`
		Version   string `json:"version"`
		Instalado bool   `json:"instalado"`
		Retomar   bool   `json:"retomar"`
		Derivar   bool   `json:"derivar"`
	}
	lista := []ag{}
	for _, n := range []string{"claude", "codex"} {
		a, _ := adapters.Para(n)
		item := ag{Nombre: n}
		if inst, err := a.Detectar(); err == nil {
			item.Instalado, item.Ruta, item.Version = true, inst.Ruta, inst.Version
			c := a.Capacidades()
			item.Retomar, item.Derivar = c.Retomar, c.Derivar
		}
		lista = append(lista, item)
	}
	out.ok(lista, func() {
		for _, a := range lista {
			if a.Instalado {
				fmt.Printf("%-7s v%-20s %s\n", a.Nombre, a.Version, a.Ruta)
			} else {
				fmt.Printf("%-7s no encontrado\n", a.Nombre)
			}
		}
	})
}

func entorno(out salida, cfg config.Config) {
	res := map[string]any{
		"raiz": cfg.Raiz, "db": cfg.DB, "worker": cfg.Worker,
		"marca":              config.RutaMarca(cfg),
		"aviso_reactivacion": platform.AvisoReactivacion(),
		"version":            version,
	}
	out.ok(res, func() {
		for k, v := range res {
			fmt.Printf("%-20s %v\n", k, v)
		}
	})
}

// ---------- auxiliares ----------

func arg(out salida, a []string, i int) string {
	if i >= len(a) {
		out.fallo(fmt.Errorf("falta un argumento"))
	}
	return a[i]
}

func nombreDeRuta(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return filepath.Base(p)
}

func nombreDe(t *store.Task) string {
	if t == nil {
		return ""
	}
	return t.Name
}
func agenteDe(t *store.Task) string {
	if t == nil {
		return ""
	}
	return t.Agent
}
func zonaDe(t *store.Task) string {
	if t == nil {
		return ""
	}
	return t.Timezone
}
func perfilDe(t *store.Task) string {
	if t == nil {
		return ""
	}
	return t.PermissionProfile
}
func modoDe(t *store.Task) string {
	if t == nil {
		return ""
	}
	return t.ConversationMode
}
func politicaDe(t *store.Task) string {
	if t == nil {
		return ""
	}
	return t.MisfirePolicy
}

// defaultStr2 normaliza el modo de workspace vacío a "isolated".
func defaultStr2(v string) string {
	if v == "" {
		return "isolated"
	}
	return v
}
