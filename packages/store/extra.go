package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Tipos que la línea de órdenes usa para construir tareas sin importar
// database/sql.
type NullString = sql.NullString

func NullFloat(v float64) sql.NullFloat64 {
	if v <= 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}

// ---------- sesiones de agente ----------

type SessionRef struct {
	ID, Provider, ExternalID, CWD string
}

func (db *DB) UpsertSessionRef(provider, externalID, cwd string) (string, error) {
	var id string
	err := db.QueryRow(`SELECT id FROM session_refs WHERE provider=? AND external_session_id=?`,
		provider, externalID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = NewID()
	_, err = db.Exec(`INSERT INTO session_refs (id,provider,external_session_id,cwd,last_seen_at)
	                  VALUES (?,?,?,?,?)`, id, provider, externalID, cwd, Now())
	return id, err
}

func (db *DB) GetSessionRef(id string) (*SessionRef, error) {
	s := &SessionRef{}
	err := db.QueryRow(`SELECT id,provider,external_session_id,cwd FROM session_refs WHERE id=?`, id).
		Scan(&s.ID, &s.Provider, &s.ExternalID, &s.CWD)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ---------- tareas ----------

// UpdateTask guarda los campos editables y, si el prompt cambió, crea una
// revisión nueva. Las ejecuciones anteriores conservan la que usaron.
func (db *DB) UpdateTask(t Task, prompt string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	if _, err := tx.Exec(`UPDATE tasks SET name=?, agent=?, conversation_mode=?, session_ref_id=?,
		schedule_rule=?, timezone=?, misfire_policy=?, max_lateness_seconds=?, permission_profile=?,
		timeout_seconds=?, max_budget_usd=?, daily_budget_usd=?, autocompact=?, workspace_mode=?,
		depends_on_task_id=?, trigger_on=?, updated_at=? WHERE id=?`,
		t.Name, t.Agent, t.ConversationMode, t.SessionRefID, t.ScheduleRule, t.Timezone,
		t.MisfirePolicy, maxOr(t.MaxLatenessSeconds, 7200), t.PermissionProfile, t.TimeoutSeconds,
		t.MaxBudgetUSD, t.DailyBudgetUSD, t.Autocompact, defaultStr(t.WorkspaceMode, "isolated"),
		t.DependsOnTaskID, t.TriggerOn, now, t.ID); err != nil {
		return err
	}
	var actual string
	var version int
	if err := tx.QueryRow(`SELECT prompt_text, version FROM task_revisions WHERE task_id=?
	                       ORDER BY version DESC LIMIT 1`, t.ID).Scan(&actual, &version); err != nil {
		return err
	}
	if actual != prompt {
		if _, err := tx.Exec(`INSERT INTO task_revisions (id,task_id,version,prompt_text,prompt_sha256,created_at)
		                      VALUES (?,?,?,?,?,?)`,
			NewID(), t.ID, version+1, prompt, Sha256(prompt), now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO audit (task_id,at,actor,action,detail_json) VALUES (?,?,?,?,?)`,
		t.ID, now, "user", "task_updated", `{}`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	db.notify()
	return nil
}

func (db *DB) SetEnabled(id string, enabled bool) error {
	_, err := db.Exec(`UPDATE tasks SET enabled=?, updated_at=? WHERE id=?`, boolToInt(enabled), Now(), id)
	if err == nil {
		db.notify()
	}
	return err
}

// LastRunState devuelve el estado y la fecha de la última ejecución de la tarea.
func (db *DB) LastRunState(taskID string) (string, string, error) {
	var estado string
	var cuando sql.NullString
	err := db.QueryRow(`SELECT status, COALESCE(finished_at, started_at, scheduled_for_utc)
		FROM runs WHERE task_id=? ORDER BY rowid DESC LIMIT 1`, taskID).Scan(&estado, &cuando)
	if err != nil {
		return "", "", err
	}
	return estado, cuando.String, nil
}

// ---------- ejecuciones con detalle ----------

type RunDetalle struct {
	ID, TaskID, TaskName, ProjectName, WorkspacePath, GitRoot, DefaultBranch, Agent string
	Status                                                                          State
	ScheduledForUTC, StartedAt, FinishedAt                                          string
	WorktreePath, WorktreeBranch, BaseCommit                                        string
	CostUSD                                                                         sql.NullFloat64
	Summary, ErrorCode, ProviderSession, ReviewDecision                             string
	WorkspaceMode                                                                   string
	CancelRequested                                                                 bool
}

const selectDetalle = `
	SELECT r.id, r.task_id, t.name, p.name, p.workspace_path, p.git_root, p.default_branch, t.agent, t.workspace_mode,
	       r.status, r.scheduled_for_utc, COALESCE(r.started_at,''), COALESCE(r.finished_at,''),
	       COALESCE(r.worktree_path,''), COALESCE(r.worktree_branch,''), COALESCE(r.base_commit,''),
	       r.cost_usd, COALESCE(r.summary,''), COALESCE(r.error_code,''),
	       COALESCE(r.provider_session_id,''), COALESCE(r.review_decision,''), r.cancel_requested
	FROM runs r
	JOIN tasks t    ON t.id = r.task_id
	JOIN projects p ON p.id = t.project_id`

func scanDetalle(rows interface{ Scan(...any) error }) (*RunDetalle, error) {
	r := &RunDetalle{}
	var cancel int
	err := rows.Scan(&r.ID, &r.TaskID, &r.TaskName, &r.ProjectName, &r.WorkspacePath, &r.GitRoot,
		&r.DefaultBranch, &r.Agent, &r.WorkspaceMode, &r.Status, &r.ScheduledForUTC, &r.StartedAt, &r.FinishedAt,
		&r.WorktreePath, &r.WorktreeBranch, &r.BaseCommit, &r.CostUSD, &r.Summary, &r.ErrorCode,
		&r.ProviderSession, &r.ReviewDecision, &cancel)
	if err != nil {
		return nil, err
	}
	r.CancelRequested = cancel != 0
	return r, nil
}

func (db *DB) GetRunDetalle(id string) (*RunDetalle, error) {
	return scanDetalle(db.QueryRow(selectDetalle+` WHERE r.id=?`, id))
}

func (db *DB) InboxDetalle() ([]RunDetalle, error) {
	rows, err := db.Query(selectDetalle + `
		WHERE r.review_decision IS NULL
		  AND r.status IN ('awaiting_review','completed','failed','failed_quota','failed_auth',
		                   'failed_verification','running','preflight','queued','verifying')
		ORDER BY (r.status IN ('running','preflight','queued','verifying')) DESC,
		         COALESCE(r.finished_at, r.started_at, r.scheduled_for_utc) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunDetalle
	for rows.Next() {
		r, err := scanDetalle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (db *DB) Historial(limite int) ([]RunDetalle, error) {
	rows, err := db.Query(selectDetalle+`
		ORDER BY COALESCE(r.finished_at, r.started_at, r.scheduled_for_utc) DESC LIMIT ?`, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunDetalle
	for rows.Next() {
		r, err := scanDetalle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (db *DB) SetReview(runID, decision string) error {
	_, err := db.Exec(`UPDATE runs SET review_decision=?, review_at=? WHERE id=?`, decision, Now(), runID)
	if err == nil {
		db.notify()
	}
	return err
}

// ---------- eventos ----------

type Evento struct {
	Sequence  int    `json:"seq"`
	Type      string `json:"tipo"`
	Payload   string `json:"payload"`
	CreatedAt string `json:"en"`
}

func (db *DB) Eventos(runID string, desde int) ([]Evento, error) {
	rows, err := db.Query(`SELECT sequence,event_type,payload_json,created_at FROM run_events
	                       WHERE run_id=? AND sequence>? ORDER BY sequence`, runID, desde)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Evento
	for rows.Next() {
		var e Evento
		if err := rows.Scan(&e.Sequence, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EsActivo dice si una ejecución sigue viva (no se puede archivar ni decidir).
func EsActivo(s State) bool {
	switch s {
	case StateQueued, StatePreflight, StateRunning, StateVerifying:
		return true
	}
	return false
}

// YaHayReintentoPorCuota dice si esta tarea ya programó un reintento por cuota
// en las últimas 24 horas. Sirve para no encadenar reintentos.
func (db *DB) YaHayReintentoPorCuota(taskID string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM audit WHERE task_id=? AND action='quota_retry_scheduled'
	             AND at > datetime('now','-1 day')`, taskID).Scan(&n)
	return n > 0
}

// defaultStr devuelve d si v está vacío.
func defaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// ---------- resumen matinal / salud del planificador (§27.1) ----------

type ResumenNoche struct {
	DesdeUTC        string  `json:"desde_utc"`
	Terminadas      int     `json:"terminadas"`       // completed + accepted + rejected (resueltas)
	EsperanRevision int     `json:"esperan_revision"` // awaiting_review
	Fallidas        int     `json:"fallidas"`         // failed*
	Saltadas        int     `json:"saltadas"`         // skipped (misfire / sin turno / tope)
	EnCurso         int     `json:"en_curso"`         // running/queued/preflight/verifying
	CosteTotalUSD   float64 `json:"coste_total_usd"`
}

// ResumenNoche agrega las ejecuciones terminadas desde desdeUTC (RFC3339) más las
// que siguen en curso. El bucketing por estado se hace en Go. `rejected` cuenta en
// Terminadas para que los contadores cuadren con el coste total.
func (db *DB) ResumenNoche(desdeUTC string) (ResumenNoche, error) {
	r := ResumenNoche{DesdeUTC: desdeUTC}
	// La rama en-curso se acota también por recencia (`scheduled_for_utc>=?1`): sin
	// ese límite, una run huérfana atascada en `queued` (p. ej. una pendiente-manual
	// que nunca se resuelve) se contaría como «en curso» noche tras noche.
	rows, err := db.Query(`SELECT status, cost_usd FROM runs
		WHERE finished_at >= ?1
		   OR (status IN ('running','queued','preflight','verifying') AND scheduled_for_utc >= ?1)`, desdeUTC)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var cost sql.NullFloat64
		if err := rows.Scan(&st, &cost); err != nil {
			return r, err
		}
		switch State(st) {
		case StateCompleted, StateAccepted, StateRejected:
			r.Terminadas++
		case StateAwaitingReview:
			r.EsperanRevision++
		case StateSkipped:
			r.Saltadas++
		case StateRunning, StateQueued, StatePreflight, StateVerifying:
			r.EnCurso++
		case StateCancelled:
			// Terminal con posible coste; se cuenta como fallida (coherente con
			// FAILED_STATES del front) para que los contadores cuadren con el coste.
			r.Fallidas++
		default:
			if strings.HasPrefix(st, "failed") {
				r.Fallidas++
			}
		}
		if cost.Valid {
			r.CosteTotalUSD += cost.Float64
		}
	}
	return r, rows.Err()
}

type SaludBaseTarea struct {
	TareaID          string
	Tarea            string
	DisparosPerdidos int // skipped POR MISFIRE (no por sin-turno ni tope de gasto)
	Pendientes       int // pendientes de decisión manual (misfire con política 'manual')
}

// SaludBase, por cada tarea activa: disparos perdidos por MISFIRE y pendientes de
// decisión manual en la ventana [desdeUTC, ahora]. `skipped` tiene varios orígenes
// (misfire, sin turno libre, tope de gasto §27.2) y todos comparten estado; solo el
// de misfire cuenta aquí, y se distingue por el detalle de auditoría del run saltado.
// Nota: los audits de transición (`state_skipped`) guardan task_id NULL, así que se
// unen por `run_id`; los `pendiente_de_decision` sí llevan task_id.
func (db *DB) SaludBase(desdeUTC string) ([]SaludBaseTarea, error) {
	// 1) tareas activas (ids + nombres); se cierra el cursor antes de más consultas
	//    (una sola conexión: consultar dentro del recorrido bloquearía).
	type tn struct{ id, name string }
	// Excluye las dependientes (§27.4): están activas pero sin disparador del SO por
	// diseño, así que no deben salir como «disparador ausente» en la salud.
	rows, err := db.Query(`SELECT id, name FROM tasks
		WHERE enabled=1 AND depends_on_task_id='' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	var tareas []tn
	for rows.Next() {
		var t tn
		if err := rows.Scan(&t.id, &t.name); err != nil {
			rows.Close()
			return nil, err
		}
		tareas = append(tareas, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 2) disparos perdidos: runs skipped cuyo motivo de auditoría habla de retraso.
	perdidos, err := db.cuentaPorTarea(`SELECT r.task_id, COUNT(*) FROM runs r
		JOIN audit a ON a.run_id=r.id AND a.action='state_skipped'
		WHERE r.status='skipped' AND a.at>=?1 AND a.detail_json LIKE '%"motivo":"misfire"%'
		GROUP BY r.task_id`, desdeUTC)
	if err != nil {
		return nil, err
	}
	// 3) pendientes de decisión manual (este audit sí lleva task_id).
	pend, err := db.cuentaPorTarea(`SELECT task_id, COUNT(*) FROM audit
		WHERE action='pendiente_de_decision' AND task_id IS NOT NULL AND at>=?1
		GROUP BY task_id`, desdeUTC)
	if err != nil {
		return nil, err
	}
	out := make([]SaludBaseTarea, 0, len(tareas))
	for _, t := range tareas {
		out = append(out, SaludBaseTarea{TareaID: t.id, Tarea: t.name,
			DisparosPerdidos: perdidos[t.id], Pendientes: pend[t.id]})
	}
	return out, nil
}

// ---------- encadenado (§27.4) ----------

// DependientesDe devuelve las tareas ACTIVAS que dependen de taskID y cuyo disparo
// casa con el resultado ('success'|'failure'); 'always' casa con cualquiera. Sigue
// el patrón de ListTasks: recoge ids con el cursor y luego carga cada tarea (una
// sola conexión).
func (db *DB) DependientesDe(taskID, resultado string) ([]*Task, error) {
	rows, err := db.Query(`SELECT id FROM tasks
		WHERE enabled=1 AND depends_on_task_id=? AND (trigger_on=? OR trigger_on='always')
		ORDER BY created_at`, taskID, resultado)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Task, 0, len(ids))
	for _, id := range ids {
		t, err := db.GetTask(id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// CuentaDependientes dice cuántas tareas dependen de taskID (§27.4). Borrar un
// padre con dependientes las dejaría huérfanas (sin disparador y sin quien las
// dispare), así que se bloquea en la capa de órdenes.
func (db *DB) CuentaDependientes(taskID string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE depends_on_task_id=?`, taskID).Scan(&n)
	return n, err
}

// CuentaRunsPendientes cuenta ejecuciones de la tarea que esperan tu decisión
// (awaiting_review) o siguen vivas. Borrar la tarea con estas pendientes dejaría
// worktrees huérfanos en disco (nunca se descartan) y perdería el registro
// revisable, así que se bloquea el borrado hasta resolverlas.
func (db *DB) CuentaRunsPendientes(taskID string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM runs WHERE task_id=? AND status IN
		('awaiting_review','queued','preflight','running','verifying')`, taskID).Scan(&n)
	return n, err
}

// ---------- gasto (§27.2) ----------

type GastoTarea struct {
	TareaID string  `json:"tarea_id"`
	Tarea   string  `json:"tarea"`
	Runs    int     `json:"runs"`
	USD     float64 `json:"usd"`
}
type GastoDia struct {
	Dia string  `json:"dia"`
	USD float64 `json:"usd"`
}

// GastoDesde suma el coste INFORMADO por el agente desde desdeUTC (RFC3339). Solo
// cuenta runs con cost_usd no nulo; Codex no reporta coste, así que sus runs no
// suman (limitación conocida: la UI lo advierte).
func (db *DB) GastoDesde(desdeUTC string) (float64, error) {
	var v sql.NullFloat64
	err := db.QueryRow(`SELECT COALESCE(SUM(cost_usd),0) FROM runs
		WHERE cost_usd IS NOT NULL AND finished_at >= ?`, desdeUTC).Scan(&v)
	return v.Float64, err
}

// GastoDiaTarea suma el coste de UNA tarea desde desdeUTC (para el tope diario).
func (db *DB) GastoDiaTarea(taskID, desdeUTC string) (float64, error) {
	var v sql.NullFloat64
	err := db.QueryRow(`SELECT COALESCE(SUM(cost_usd),0) FROM runs
		WHERE task_id=? AND cost_usd IS NOT NULL AND finished_at >= ?`, taskID, desdeUTC).Scan(&v)
	return v.Float64, err
}

func (db *DB) GastoPorTarea(desdeUTC string) ([]GastoTarea, error) {
	rows, err := db.Query(`SELECT r.task_id, t.name, COUNT(*), COALESCE(SUM(r.cost_usd),0)
		FROM runs r JOIN tasks t ON t.id=r.task_id
		WHERE r.cost_usd IS NOT NULL AND r.finished_at >= ?1
		GROUP BY r.task_id ORDER BY SUM(r.cost_usd) DESC`, desdeUTC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GastoTarea{}
	for rows.Next() {
		var g GastoTarea
		if err := rows.Scan(&g.TareaID, &g.Tarea, &g.Runs, &g.USD); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (db *DB) GastoPorDia(desdeUTC string) ([]GastoDia, error) {
	rows, err := db.Query(`SELECT substr(finished_at,1,10), COALESCE(SUM(cost_usd),0)
		FROM runs WHERE cost_usd IS NOT NULL AND finished_at >= ?1
		GROUP BY substr(finished_at,1,10) ORDER BY 1`, desdeUTC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GastoDia{}
	for rows.Next() {
		var g GastoDia
		if err := rows.Scan(&g.Dia, &g.USD); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// cuentaPorTarea ejecuta una consulta `SELECT task_id, COUNT(*)` y la vuelca a un mapa.
func (db *DB) cuentaPorTarea(q, arg string) (map[string]int, error) {
	m := map[string]int{}
	rows, err := db.Query(q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullString
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		if id.Valid {
			m[id.String] = n
		}
	}
	return m, rows.Err()
}
