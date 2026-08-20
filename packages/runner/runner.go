// Package runner ejecuta una ocurrencia de principio a fin.
//
// Es lo que arranca el programador del sistema operativo. No hay proceso
// residente: este código vive lo que dura una ejecución y se va.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/argalla/taskkeeper/adapters"
	"github.com/argalla/taskkeeper/packages/gitwt"
	"github.com/argalla/taskkeeper/packages/platform"
	"github.com/argalla/taskkeeper/packages/scheduler"
	"github.com/argalla/taskkeeper/packages/store"
	"github.com/argalla/taskkeeper/packages/turns"
)

type Opciones struct {
	CupoSimultaneo int
	DirTurnos      string
	DirWorktrees   string
	EsperaTurno    time.Duration
	SondeoCancel   time.Duration
	// Cuánto esperar para reintentar por cuota cuando el proveedor no dice
	// cuándo vuelve.
	EsperaCuotaPorDefecto time.Duration
	// Registra en el sistema un disparador puntual para reintentar la tarea.
	// Nil desactiva el reintento (pruebas, plataformas sin implementación).
	RegistrarReintento func(taskID string, cuando time.Time) error
	// Lanza (fire-and-forget) una tarea dependiente cuando la padre termina
	// (§27.4). Nil desactiva el encadenado (pruebas, plataformas sin worker).
	LanzarDependiente func(taskID string) error
}

func PorDefecto() Opciones {
	return Opciones{
		CupoSimultaneo:        1,
		EsperaTurno:           30 * time.Minute,
		SondeoCancel:          3 * time.Second,
		EsperaCuotaPorDefecto: time.Hour,
	}
}

// Deps permite sustituir el agente y el grupo de procesos en las pruebas, para
// no gastar cuota real cada vez que se comprueba la cancelación.
type Deps struct {
	Adaptador func(nombre string) (adapters.Adaptador, error)
	Grupo     func() (platform.GrupoProcesos, error)
}

func DepsReales() Deps {
	return Deps{Adaptador: adapters.Para, Grupo: platform.NuevoGrupo}
}

// EjecutarProgramada es el punto de entrada del disparador del sistema.
//
// El sistema operativo no dice de qué hora viene, así que la ocurrencia se
// DERIVA de la regla de la tarea. Suponer "ahora" tenía dos consecuencias malas:
// no se podía saber si la ejecución llegaba tarde, y la clave de idempotencia
// nunca coincidía, así que la protección contra duplicados no protegía nada en
// las ejecuciones reales.
//
// Con la ocurrencia derivada, además, las dos políticas de retraso dejan de
// pelearse: el sistema nos despierta aunque sea tarde y aquí se decide si
// merece la pena ejecutar.
func EjecutarProgramada(ctx context.Context, db *store.DB, d Deps, o Opciones,
	taskID string, ahora time.Time) error {

	task, err := db.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("tarea %s: %w", taskID, err)
	}
	if !task.Enabled {
		return nil
	}

	var regla scheduler.Rule
	if err := json.Unmarshal([]byte(task.ScheduleRule), &regla); err != nil {
		return fmt.Errorf("regla de calendario de %s: %w", taskID, err)
	}
	occ, err := scheduler.Previous(regla, task.Timezone, ahora)
	if err != nil {
		return fmt.Errorf("calculando la ocurrencia de %s: %w", taskID, err)
	}
	if occ == nil {
		// Ninguna hora prevista ha pasado todavía: nada que ejecutar.
		return nil
	}

	maxRetraso := time.Duration(task.MaxLatenessSeconds) * time.Second
	switch scheduler.ResolveMisfire(occ.ScheduledForUTC, ahora,
		scheduler.MisfirePolicy(task.MisfirePolicy), maxRetraso) {

	case scheduler.DecisionSkip:
		// Se deja constancia de la omisión: que no se ejecutase también es
		// información, y sin ella el usuario no entiende por qué no pasó nada.
		if run, creada, err := db.CreateRunIfAbsent(taskID, revID(db, taskID), occ.ScheduledForUTC); err == nil && creada {
			// Motivo estructurado: la vista de salud (§27.1) lo distingue del salto
			// por «sin turno libre» sin depender de la prosa del mensaje.
			db.Transition(run.ID, store.StateSkipped,
				fmt.Sprintf(`{"motivo":"misfire","detalle":"retraso de %s sobre la hora prevista"}`,
					ahora.Sub(occ.ScheduledForUTC).Round(time.Minute)))
		}
		return nil

	case scheduler.DecisionPending:
		if run, creada, err := db.CreateRunIfAbsent(taskID, revID(db, taskID), occ.ScheduledForUTC); err == nil && creada {
			db.Audit(run.ID, taskID, "system", "pendiente_de_decision",
				`{"motivo":"llegó tarde y la política es manual"}`)
		}
		return nil
	}

	return Ejecutar(ctx, db, d, o, taskID, occ.ScheduledForUTC)
}

func revID(db *store.DB, taskID string) string {
	if r, err := db.LatestRevision(taskID); err == nil {
		return r.ID
	}
	return ""
}

// topeSuperado devuelve un motivo (JSON, para no confundirse con el misfire de la
// vista de salud) si un tope de gasto impide ejecutar, o "" si se puede seguir.
// El tope mensual es de máquina; el diario, por tarea (campo daily_budget_usd, que
// hasta ahora se guardaba pero no se aplicaba).
func topeSuperado(db *store.DB, task *store.Task, ahora time.Time) string {
	if tope := db.TopeMes(); tope > 0 {
		inicioMes := time.Date(ahora.Year(), ahora.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		if g, err := db.GastoDesde(inicioMes); err == nil && g >= tope {
			return fmt.Sprintf(`{"motivo":"tope_mensual","detalle":"tope mensual de %.2f USD alcanzado (%.2f gastados)"}`, tope, g)
		}
	}
	if task.DailyBudgetUSD.Valid && task.DailyBudgetUSD.Float64 > 0 {
		inicioDia := time.Date(ahora.Year(), ahora.Month(), ahora.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		if g, err := db.GastoDiaTarea(task.ID, inicioDia); err == nil && g >= task.DailyBudgetUSD.Float64 {
			return fmt.Sprintf(`{"motivo":"tope_diario","detalle":"tope diario de %.2f USD alcanzado (%.2f hoy)"}`, task.DailyBudgetUSD.Float64, g)
		}
	}
	return ""
}

// esFallo dice si un estado terminal es un fallo «real» (no cuota, que reintenta).
func esFallo(s store.State) bool {
	switch s {
	case store.StateFailed, store.StateFailedVerif, store.StateFailedAuth:
		return true
	}
	return false
}

// dispararDependientes lanza (fire-and-forget) las tareas que dependen de padreID
// según su estado terminal. `completed` (éxito directo) → dependientes 'success';
// fallos reales → 'failure'; `failed_quota` no dispara (tiene reintento); el resto
// (skipped/cancelled/awaiting_review) no encadena — lo que deja los padres AISLADOS
// fuera de v1 (su éxito = «aceptar», que llegará por `ctl aceptar` más adelante).
func dispararDependientes(db *store.DB, o Opciones, padreID string, estado store.State) {
	if o.LanzarDependiente == nil {
		return
	}
	var res string
	switch {
	case estado == store.StateCompleted:
		res = "success"
	case estado == store.StateFailedQuota:
		return
	case esFallo(estado):
		res = "failure"
	default:
		return
	}
	deps, err := db.DependientesDe(padreID, res)
	if err != nil {
		return
	}
	for _, dep := range deps {
		// Deja constancia del vínculo causal (padre→hija) y del resultado del
		// lanzamiento: si el worker no arranca, la cadena se rompe con rastro.
		if err := o.LanzarDependiente(dep.ID); err != nil {
			db.Audit("", padreID, "system", "dependent_dispatch_failed",
				fmt.Sprintf(`{"hija":%q,"resultado":%q,"error":%q}`, dep.ID, res, err.Error()))
		} else {
			db.Audit("", padreID, "system", "dependent_dispatched",
				fmt.Sprintf(`{"hija":%q,"resultado":%q}`, dep.ID, res))
		}
	}
}

// Ejecutar procesa una ocurrencia concreta. Lo usa EjecutarProgramada con la
// hora derivada, y el arranque manual con el instante actual: así una prueba a
// mano no consume la ocurrencia programada de esta noche.
//
// Nunca devuelve error por un fallo de la tarea: los fallos de la tarea son
// estados, no errores del programa. Solo devuelve error si no pudo ni siquiera
// registrar lo ocurrido.
func Ejecutar(ctx context.Context, db *store.DB, d Deps, o Opciones,
	taskID string, ocurrencia time.Time) error {

	task, err := db.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("tarea %s: %w", taskID, err)
	}
	rev, err := db.LatestRevision(taskID)
	if err != nil {
		return fmt.Errorf("revisión de %s: %w", taskID, err)
	}

	// 1. Idempotencia. Si esta ocurrencia ya tiene ejecución, este proceso se
	//    retira en silencio: es el caso del disparo de recuperación que llega
	//    después del normal.
	run, creada, err := db.CreateRunIfAbsent(taskID, rev.ID, ocurrencia)
	if err != nil {
		return err
	}
	if !creada {
		return nil
	}

	// §27.4 — al terminar (por cualquier camino), dispara las tareas que dependen
	// de esta según su resultado. Va por defer porque `Ejecutar` tiene muchos
	// `return nil` tempranos: el éxito directo (`completed`) retorna antes del final,
	// y una llamada única al cierre se los perdería. Solo dispara con `run` creada.
	defer func() {
		if r, e := db.GetRun(run.ID); e == nil {
			dispararDependientes(db, o, task.ID, r.Status)
		}
	}()

	// 1b. Topes de gasto (§27.2). Se comprueban aquí, en `queued`, ANTES de tomar
	//     turno o crear worktree: un salto por tope no debe consumir recursos. La
	//     ventana se ancla en la hora REAL (como el panel), no en la ocurrencia, para
	//     que el tope diario de una tarea de madrugada no se desfase. Solo el coste
	//     informado por el agente cuenta: Codex no lo reporta — la UI lo advierte.
	if motivo := topeSuperado(db, task, time.Now().UTC()); motivo != "" {
		db.Transition(run.ID, store.StateSkipped, motivo)
		return nil
	}

	// 2. Turno. Sin coordinador central, el cupo se reparte con ficheros.
	slot, err := turns.Acquire(o.CupoSimultaneo, o.DirTurnos, o.EsperaTurno)
	if err != nil {
		db.Transition(run.ID, store.StateSkipped, "sin turno libre dentro del plazo")
		return nil
	}
	defer slot.Release()

	if db.CancelRequested(run.ID) {
		db.Transition(run.ID, store.StateCancelled, "cancelada antes de empezar")
		return nil
	}

	// Re-comprobación del tope tras esperar turno: con cupo=1 la run anterior pudo
	// terminar mientras esperábamos y dejar el mes por encima del tope. Cierra la
	// fuga de «una run extra por worker en espera».
	if motivo := topeSuperado(db, task, time.Now().UTC()); motivo != "" {
		db.Transition(run.ID, store.StateSkipped, motivo)
		return nil
	}

	// 3. Preflight. Todo lo que falle aquí es seguro: no se ha lanzado nada.
	if err := db.Transition(run.ID, store.StatePreflight, ""); err != nil {
		return err
	}
	proj, err := db.GetProject(task.ProjectID)
	if err != nil {
		db.Transition(run.ID, store.StateFailed, err.Error())
		return nil
	}
	ad, err := d.Adaptador(task.Agent)
	if err != nil {
		db.Transition(run.ID, store.StateFailed, err.Error())
		return nil
	}
	// La ruta del agente se resuelve AHORA, no se confía en la guardada: caduca
	// en cuanto VS Code actualiza la extensión.
	if _, err := ad.Detectar(); err != nil {
		db.Transition(run.ID, store.StateFailedAuth, err.Error())
		return nil
	}
	// 4. Preparación del sitio de trabajo, según el modo.
	//
	// Modo «en la conversación» (directo): no se crea worktree ni se exige un
	// repositorio Git; el agente trabaja en el repo real y no hay revisión. Solo
	// se comprueba que la carpeta exista. El modo aislado es el camino de siempre:
	// exige repo y rama, y crea el worktree desde el commit base.
	directo := task.WorkspaceMode == "direct"
	var wt *gitwt.Worktree
	if directo {
		if _, err := gitwt.ComprobarSuave(ctx, proj.WorkspacePath); err != nil {
			db.Transition(run.ID, store.StateFailed, err.Error())
			return nil
		}
	} else {
		pf, err := gitwt.Comprobar(ctx, proj.WorkspacePath, proj.DefaultBranch)
		if err != nil {
			db.Transition(run.ID, store.StateFailed, err.Error())
			return nil
		}
		if pf.Sucio {
			db.AppendEvent(run.ID, "aviso",
				`{"aviso":"el checkout principal tiene cambios sin guardar; no se copian al worktree"}`)
		}
		rama, err := gitwt.NombreRama(task.Name, ocurrencia, run.ID[:6])
		if err != nil {
			db.Transition(run.ID, store.StateFailed, err.Error())
			return nil
		}
		wt, err = gitwt.Crear(ctx, pf, o.DirWorktrees, rama)
		if err != nil {
			db.Transition(run.ID, store.StateFailed, err.Error())
			return nil
		}
		db.SetRunField(run.ID, "worktree_path", wt.Path)
		db.SetRunField(run.ID, "worktree_branch", wt.Branch)
		db.SetRunField(run.ID, "base_commit", wt.Base)
	}

	// 5. El agente, dentro de su grupo de procesos.
	grupo, err := d.Grupo()
	if err != nil {
		db.Transition(run.ID, store.StateFailed, err.Error())
		return nil
	}
	// Cierre del grupo = muerte de toda la descendencia. Es la red de seguridad
	// si este proceso se va sin cancelar nada.
	defer grupo.Close()

	// Continuar/derivar necesitan el id de sesión externo del proveedor, que vive
	// en el session_ref. Sin resolverlo, el agente arranca con `--resume` vacío y
	// falla con "requires a valid session ID" antes de dar una sola vuelta.
	var sesionExterna string
	if m := adapters.Modo(task.ConversationMode); m == adapters.ModoRetomar || m == adapters.ModoDerivar {
		if !task.SessionRefID.Valid || task.SessionRefID.String == "" {
			db.SetRunField(run.ID, "error_code", "sin_referencia_sesion")
			db.Transition(run.ID, store.StateFailed, "la tarea continúa o deriva una conversación pero no guarda su referencia de sesión")
			return nil
		}
		ref, err := db.GetSessionRef(task.SessionRefID.String)
		if err != nil || ref == nil || ref.ExternalID == "" {
			db.SetRunField(run.ID, "error_code", "sesion_no_resuelta")
			db.Transition(run.ID, store.StateFailed, "no se pudo resolver la conversación a continuar o derivar (session_ref inválido)")
			return nil
		}
		sesionExterna = ref.ExternalID
	}

	cmd, err := ad.Comando(adapters.Peticion{
		Prompt:         rev.Prompt,
		Modo:           adapters.Modo(task.ConversationMode),
		SesionExterna:  sesionExterna,
		NuevaSesionID:  uuidDe(run.ID),
		DirTrabajo:     proj.WorkspacePath,
		Worktree:       rutaWorktree(wt),
		Perfil:         adapters.Perfil(task.PermissionProfile),
		MaxTurnos:      40,
		MaxPresupuesto: nz(task.MaxBudgetUSD.Float64, task.MaxBudgetUSD.Valid),
		Autocompact:    task.Autocompact,
	})
	if err != nil {
		db.Transition(run.ID, store.StateFailed, err.Error())
		return nil
	}
	if directo {
		// Directo: el agente corre en el repo real (para auditoría es de solo
		// lectura; para cambios, escribe ahí mismo — decisión explícita del
		// usuario, avisada en el panel).
		cmd.Dir = proj.WorkspacePath
	} else if task.PermissionProfile == string(adapters.PerfilAislado) {
		cmd.Dir = wt.Path
	}

	salida, err := cmd.StdoutPipe()
	if err != nil {
		db.Transition(run.ID, store.StateFailed, err.Error())
		return nil
	}
	grupo.Prepare(cmd)
	if err := cmd.Start(); err != nil {
		db.Transition(run.ID, store.StateFailed, err.Error())
		return nil
	}
	// En la instrucción siguiente al arranque: la ventana de carrera es la
	// mínima posible sin recurrir a CreateProcess suspendido.
	if err := grupo.Adopt(cmd.Process.Pid); err != nil {
		grupo.KillAll()
		db.Transition(run.ID, store.StateFailed, "no se pudo aislar el proceso: "+err.Error())
		return nil
	}
	db.Transition(run.ID, store.StateRunning, "")

	des := consumir(ctx, db, run.ID, ad, grupo, salida, cmd,
		time.Duration(task.TimeoutSeconds)*time.Second, o.SondeoCancel)

	// 6. Cerrar según el desenlace.
	switch des.Estado {
	case string(store.StateVerifying):
		if directo {
			// «En la conversación»: no hay worktree ni diff que revisar; el trabajo
			// ya está en tu repo y sus turnos en la conversación. Termina en
			// `completed`, un estado final sin Aceptar/Rechazar.
			resumen, _ := json.Marshal(map[string]any{"ficheros": nil, "modo": "en_conversacion"})
			if des.CosteUSD != nil {
				db.SetRunField(run.ID, "cost_usd", *des.CosteUSD)
			}
			db.SetRunField(run.ID, "summary", string(resumen))
			db.SetRunField(run.ID, "stop_subtype", des.Subtipo)
			db.Transition(run.ID, store.StateCompleted, "")
			return nil
		}
		ficheros, diff, err := wt.Cambios(ctx)
		if err != nil {
			db.Transition(run.ID, store.StateFailed, err.Error())
			return nil
		}
		if len(ficheros) > 0 {
			if _, err := wt.Confirmar(ctx, "TaskKeeper: "+task.Name); err != nil {
				db.Transition(run.ID, store.StateFailed, err.Error())
				return nil
			}
		}
		resumen, _ := json.Marshal(map[string]any{
			"ficheros": ficheros, "bytes_diff": len(diff),
		})
		db.Transition(run.ID, store.StateVerifying, "")
		db.SetRunField(run.ID, "summary", string(resumen))
		if des.CosteUSD != nil {
			db.SetRunField(run.ID, "cost_usd", *des.CosteUSD)
		}
		db.SetRunField(run.ID, "stop_subtype", des.Subtipo)
		db.Transition(run.ID, store.StateAwaitingReview, "")
	default:
		if des.CosteUSD != nil {
			db.SetRunField(run.ID, "cost_usd", *des.CosteUSD)
		}
		db.SetRunField(run.ID, "error_code", des.Codigo)
		db.Transition(run.ID, store.State(des.Estado), des.Detalle)
		if des.Estado == string(store.StateFailedQuota) {
			programarReintentoPorCuota(db, o, task, run, des.ReintentarA)
		}
	}
	return nil
}

// programarReintentoPorCuota deja UN disparador puntual del sistema para volver
// a intentar la tarea cuando el proveedor dice que vuelve la cuota. Un solo
// reintento por tarea y ocurrencia: si vuelve a fallar por cuota, ya no se
// encadena otro. Sin demonio, el "espera y reintenta" solo puede vivir en el
// programador del sistema.
func programarReintentoPorCuota(db *store.DB, o Opciones, task *store.Task, run *store.Run, cuando *time.Time) {
	if o.RegistrarReintento == nil {
		return
	}
	if db.YaHayReintentoPorCuota(task.ID) {
		db.Audit(run.ID, task.ID, "system", "quota_retry_skipped",
			`{"motivo":"ya se reintentó una vez por cuota en las últimas 24 h"}`)
		return
	}
	t := time.Now().Add(o.EsperaCuotaPorDefecto)
	if cuando != nil && cuando.After(time.Now()) {
		t = cuando.Add(2 * time.Minute) // margen sobre lo que dice el proveedor
	}
	if err := o.RegistrarReintento(task.ID, t); err != nil {
		db.Audit(run.ID, task.ID, "system", "quota_retry_failed", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return
	}
	db.SetRunField(run.ID, "quota_reset_at", t.UTC().Format(time.RFC3339))
	db.Audit(run.ID, task.ID, "system", "quota_retry_scheduled",
		fmt.Sprintf(`{"at":%q}`, t.UTC().Format(time.RFC3339)))
}

func consumir(ctx context.Context, db *store.DB, runID string, ad adapters.Adaptador,
	grupo platform.GrupoProcesos, salida interface{ Read([]byte) (int, error) },
	cmd *exec.Cmd, timeout, sondeo time.Duration) adapters.Desenlace {

	lineas := make(chan []byte, 64)
	go func() {
		defer close(lineas)
		sc := bufio.NewScanner(salida)
		sc.Buffer(make([]byte, 1024*1024), 16*1024*1024) // las líneas JSON son largas
		for sc.Scan() {
			b := make([]byte, len(sc.Bytes()))
			copy(b, sc.Bytes())
			lineas <- b
		}
	}()

	limite := time.After(timeout)
	tic := time.NewTicker(sondeo)
	defer tic.Stop()

	var ultimo adapters.Evento
	for {
		select {
		case <-ctx.Done():
			grupo.KillAll()
			return adapters.Desenlace{Estado: string(store.StateCancelled), Detalle: "contexto cancelado"}

		case <-tic.C:
			// D7: la cancelación llega por la base, no por un canal.
			if db.CancelRequested(runID) {
				grupo.KillAll()
				return adapters.Desenlace{Estado: string(store.StateCancelled),
					Detalle: "cancelada por el usuario"}
			}

		case <-limite:
			grupo.KillAll()
			return adapters.Desenlace{Estado: string(store.StateFailed), Codigo: "timeout",
				Detalle: fmt.Sprintf("superado el límite de %s", timeout)}

		case b, ok := <-lineas:
			if !ok {
				_ = cmd.Wait()
				return desenlaceFinal(cmd, ultimo)
			}
			ev, válido := ad.Parsear(b)
			if !válido {
				continue
			}
			ultimo = ev
			db.AppendEvent(runID, string(ev.Tipo), string(b))

			switch {
			case ev.Tipo == adapters.EvSesionIniciada && ev.SesionID != "":
				db.SetRunField(runID, "provider_session_id", ev.SesionID)
			case ev.TurnoID != "":
				db.SetRunField(runID, "provider_turn_id", ev.TurnoID)
			case ev.Tipo == adapters.EvReintentoAPI:
				// Hallazgo de la Fase 0: una credencial caducada NO falla, se
				// reintenta diez veces con espera creciente. Se corta al primero,
				// o la tarea consume su timeout completo contra una pared.
				switch ev.EstadoHTTP {
				case 401, 403:
					grupo.KillAll()
					return adapters.Desenlace{Estado: string(store.StateFailedAuth),
						Codigo: ev.CodigoError, Detalle: "credencial rechazada"}
				case 429:
					grupo.KillAll()
					d := adapters.Desenlace{Estado: string(store.StateFailedQuota),
						Codigo: ev.CodigoError, Detalle: "límite de uso del proveedor"}
					if !ev.ReinicioCuota.IsZero() {
						r := ev.ReinicioCuota
						d.ReintentarA = &r
					}
					return d
				}
			}
		}
	}
}

func desenlaceFinal(cmd *exec.Cmd, ultimo adapters.Evento) adapters.Desenlace {
	if ultimo.Tipo == adapters.EvResultado {
		switch ultimo.Subtipo {
		case "success", "":
			return adapters.Desenlace{Estado: string(store.StateVerifying),
				CosteUSD: ultimo.CosteUSD, Subtipo: ultimo.Subtipo}
		case "error_max_turns", "error_max_budget_usd":
			// Corte por un límite nuestro, no fallo del agente.
			return adapters.Desenlace{Estado: string(store.StateFailed),
				Codigo: ultimo.Subtipo, CosteUSD: ultimo.CosteUSD, Subtipo: ultimo.Subtipo}
		}
	}
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if code == 0 {
		return adapters.Desenlace{Estado: string(store.StateVerifying)}
	}
	// Ante la duda, `failed`: es el estado que no reintenta y sí notifica.
	return adapters.Desenlace{Estado: string(store.StateFailed),
		Codigo: fmt.Sprintf("exit_%d", code)}
}

func nz(v float64, ok bool) float64 {
	if !ok {
		return 0
	}
	return v
}

// uuidDe convierte el identificador interno en un UUID válido, que es lo único
// que acepta --session-id.
func uuidDe(id string) string {
	if len(id) < 32 {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", id[0:8], id[8:12], id[12:16], id[16:20], id[20:32])
}

var ErrSinTarea = errors.New("tarea no encontrada")

// rutaWorktree devuelve la ruta del worktree, o vacío si no hay (modo directo).
func rutaWorktree(w *gitwt.Worktree) string {
	if w == nil {
		return ""
	}
	return w.Path
}
