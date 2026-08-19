// Package store es la base local de TaskKeeper.
//
// No hay proceso residente: cada ejecución es un worker distinto que abre la
// base, escribe poco y se va. Por eso el modo WAL y el busy_timeout no son un
// detalle de configuración, son parte del diseño.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Go puro: sin cgo, sin cadena de compilación C
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	*sql.DB
	// OnChange se invoca tras cada escritura que cambia lo que ve la interfaz.
	// El worker lo usa para tocar el fichero de marca que observa la extensión:
	// así los cambios se ven en menos de un segundo sin sondear la base.
	OnChange func()
}

func (db *DB) notify() {
	if db.OnChange != nil {
		db.OnChange()
	}
}

// Open abre la base y aplica el esquema. Es idempotente.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// _txlock=immediate evita que dos escritores empiecen una transacción
	// diferida y choquen al promocionarla.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)&_txlock=immediate"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Un worker no necesita más de una conexión y así no compite consigo mismo.
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("aplicando el esquema: %w", err)
	}
	db := &DB{DB: sqlDB}
	if err := db.migrar(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// migrar añade columnas a bases creadas por versiones anteriores. SQLite no
// tiene "ADD COLUMN IF NOT EXISTS", así que se comprueba antes.
func (db *DB) migrar() error {
	nuevas := map[string]string{
		"max_lateness_seconds": "INTEGER NOT NULL DEFAULT 7200",
	}
	filas, err := db.Query(`PRAGMA table_info(tasks)`)
	if err != nil {
		return err
	}
	existentes := map[string]bool{}
	for filas.Next() {
		var cid int
		var nombre, tipo string
		var nn, dflt, pk any
		if err := filas.Scan(&cid, &nombre, &tipo, &nn, &dflt, &pk); err != nil {
			filas.Close()
			return err
		}
		existentes[nombre] = true
	}
	filas.Close()
	for col, def := range nuevas {
		if !existentes[col] {
			if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN ` + col + ` ` + def); err != nil {
				return fmt.Errorf("añadiendo la columna %s: %w", col, err)
			}
		}
	}
	return nil
}

// ---------- ajustes de la máquina ----------
//
// El cupo de ejecuciones simultáneas es un ajuste del ordenador, uno solo para
// todo, así que vive aquí y no en la línea de órdenes de cada disparador. Antes
// cambiarlo obligaba a reescribir todos los disparadores registrados.

const ClaveCupo = "max_concurrent_runs"

func (db *DB) Ajuste(clave, pordefecto string) string {
	var v string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key=?`, clave).Scan(&v); err != nil {
		return pordefecto
	}
	return v
}

func (db *DB) FijarAjuste(clave, valor string) error {
	_, err := db.Exec(`INSERT INTO meta (key,value) VALUES (?,?)
	                   ON CONFLICT(key) DO UPDATE SET value=excluded.value`, clave, valor)
	return err
}

func (db *DB) Cupo() int {
	n, err := strconv.Atoi(db.Ajuste(ClaveCupo, "1"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// ---------- utilidades ----------

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func Sha256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// IdempotencyKey es la clave de FR-016: una tarea, un instante.
func IdempotencyKey(taskID string, scheduledForUTC time.Time) string {
	return taskID + ":" + scheduledForUTC.UTC().Format(time.RFC3339)
}

// ---------- proyectos ----------

type Project struct {
	ID, Name, WorkspacePath, GitRoot, DefaultBranch string
}

func (db *DB) UpsertProject(p Project) (string, error) {
	var id string
	err := db.QueryRow(`SELECT id FROM projects WHERE workspace_path = ?`, p.WorkspacePath).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = NewID()
	_, err = db.Exec(`INSERT INTO projects (id,name,workspace_path,git_root,default_branch,created_at)
	                  VALUES (?,?,?,?,?,?)`,
		id, p.Name, p.WorkspacePath, p.GitRoot, p.DefaultBranch, Now())
	return id, err
}

func (db *DB) GetProject(id string) (*Project, error) {
	p := &Project{}
	err := db.QueryRow(`SELECT id,name,workspace_path,git_root,default_branch FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.WorkspacePath, &p.GitRoot, &p.DefaultBranch)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ---------- tareas ----------

type Task struct {
	ID, Name, ProjectID, Agent   string
	Enabled                      bool
	ConversationMode             string
	SessionRefID                 sql.NullString
	ScheduleRule, Timezone       string
	NextRunAtUTC                 sql.NullString
	MisfirePolicy                string
	MaxLatenessSeconds           int
	PermissionProfile            string
	TimeoutSeconds               int
	MaxBudgetUSD, DailyBudgetUSD sql.NullFloat64
	OSTriggerID                  sql.NullString
}

type Revision struct {
	ID      string
	TaskID  string
	Version int
	Prompt  string
}

// CreateTask crea la tarea y su revisión 1 en una sola transacción.
func (db *DB) CreateTask(t Task, prompt string) (*Task, *Revision, error) {
	if t.ID == "" {
		t.ID = NewID()
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	now := Now()
	if _, err := tx.Exec(`INSERT INTO tasks
		(id,name,project_id,agent,enabled,conversation_mode,session_ref_id,schedule_rule,
		 timezone,next_run_at_utc,misfire_policy,max_lateness_seconds,permission_profile,
		 timeout_seconds,max_budget_usd,daily_budget_usd,os_trigger_id,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.ProjectID, t.Agent, boolToInt(t.Enabled), t.ConversationMode,
		t.SessionRefID, t.ScheduleRule, t.Timezone, t.NextRunAtUTC, t.MisfirePolicy,
		maxOr(t.MaxLatenessSeconds, 7200), t.PermissionProfile, t.TimeoutSeconds,
		t.MaxBudgetUSD, t.DailyBudgetUSD, t.OSTriggerID, now, now); err != nil {
		return nil, nil, err
	}

	rev := &Revision{ID: NewID(), TaskID: t.ID, Version: 1, Prompt: prompt}
	if _, err := tx.Exec(`INSERT INTO task_revisions (id,task_id,version,prompt_text,prompt_sha256,created_at)
	                      VALUES (?,?,?,?,?,?)`,
		rev.ID, rev.TaskID, rev.Version, prompt, Sha256(prompt), now); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(`INSERT INTO audit (task_id,at,actor,action,detail_json) VALUES (?,?,?,?,?)`,
		t.ID, now, "user", "task_created", `{"version":1}`); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	db.notify()
	return &t, rev, nil
}

func (db *DB) GetTask(id string) (*Task, error) {
	t := &Task{}
	var enabled int
	err := db.QueryRow(`SELECT id,name,project_id,agent,enabled,conversation_mode,session_ref_id,
		schedule_rule,timezone,next_run_at_utc,misfire_policy,max_lateness_seconds,
		permission_profile,timeout_seconds,max_budget_usd,daily_budget_usd,os_trigger_id
		FROM tasks WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.ProjectID, &t.Agent, &enabled, &t.ConversationMode, &t.SessionRefID,
			&t.ScheduleRule, &t.Timezone, &t.NextRunAtUTC, &t.MisfirePolicy, &t.MaxLatenessSeconds,
			&t.PermissionProfile, &t.TimeoutSeconds, &t.MaxBudgetUSD, &t.DailyBudgetUSD, &t.OSTriggerID)
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled != 0
	return t, nil
}

func (db *DB) LatestRevision(taskID string) (*Revision, error) {
	r := &Revision{TaskID: taskID}
	err := db.QueryRow(`SELECT id,version,prompt_text FROM task_revisions
	                    WHERE task_id=? ORDER BY version DESC LIMIT 1`, taskID).
		Scan(&r.ID, &r.Version, &r.Prompt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (db *DB) ListTasks() ([]*Task, error) {
	// Con una sola conexión abierta, consultar dentro del recorrido de filas
	// bloquea para siempre: primero se recogen los identificadores y se cierra
	// el cursor, después se cargan las tareas.
	rows, err := db.Query(`SELECT id FROM tasks ORDER BY created_at`)
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
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

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

func (db *DB) SetOSTriggerID(taskID, triggerID string) error {
	_, err := db.Exec(`UPDATE tasks SET os_trigger_id=?, updated_at=? WHERE id=?`,
		triggerID, Now(), taskID)
	return err
}

func (db *DB) DeleteTask(id string) error {
	_, err := db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	if err == nil {
		db.notify()
	}
	return err
}

// ---------- ejecuciones ----------

type State string

const (
	StateScheduled      State = "scheduled"
	StateQueued         State = "queued"
	StatePreflight      State = "preflight"
	StateRunning        State = "running"
	StateVerifying      State = "verifying"
	StateAwaitingReview State = "awaiting_review"
	StateAccepted       State = "accepted"
	StateRejected       State = "rejected"
	StateSkipped        State = "skipped"
	StateFailed         State = "failed"
	StateFailedVerif    State = "failed_verification"
	StateFailedQuota    State = "failed_quota"
	StateFailedAuth     State = "failed_auth"
	StateCancelled      State = "cancelled"
)

type Run struct {
	ID              string
	TaskID          string
	RevisionID      string
	ScheduledForUTC string
	Status          State
	WorktreePath    string
	WorktreeBranch  string
	BaseCommit      string
	CostUSD         sql.NullFloat64
	Summary         sql.NullString
	ErrorCode       sql.NullString
}

// CreateRunIfAbsent es la barrera de idempotencia. Devuelve created=false si esa
// ocurrencia ya tenía ejecución: el segundo disparo se retira sin hacer nada.
func (db *DB) CreateRunIfAbsent(taskID, revisionID string, scheduledFor time.Time) (*Run, bool, error) {
	key := IdempotencyKey(taskID, scheduledFor)
	run := &Run{
		ID: NewID(), TaskID: taskID, RevisionID: revisionID,
		ScheduledForUTC: scheduledFor.UTC().Format(time.RFC3339),
		Status:          StateQueued,
	}
	_, err := db.Exec(`INSERT INTO runs (id,task_id,task_revision_id,scheduled_for_utc,idempotency_key,status)
	                   VALUES (?,?,?,?,?,?)`,
		run.ID, run.TaskID, run.RevisionID, run.ScheduledForUTC, key, string(run.Status))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	db.Audit(run.ID, taskID, "system", "run_created", `{}`)
	db.notify()
	return run, true, nil
}

// transiciones permitidas: la máquina de estados vive en el código, no solo en
// el documento.
var allowed = map[State][]State{
	StateScheduled:      {StateQueued, StateSkipped, StateCancelled},
	StateQueued:         {StatePreflight, StateSkipped, StateCancelled, StateFailed},
	StatePreflight:      {StateRunning, StateFailed, StateFailedAuth, StateSkipped, StateCancelled},
	StateRunning:        {StateVerifying, StateFailed, StateFailedQuota, StateFailedAuth, StateCancelled},
	StateVerifying:      {StateAwaitingReview, StateFailedVerif, StateCancelled},
	StateAwaitingReview: {StateAccepted, StateRejected},
}

var ErrTransicionInvalida = errors.New("transición de estado no permitida")

func (db *DB) Transition(runID string, to State, detail string) error {
	var cur State
	if err := db.QueryRow(`SELECT status FROM runs WHERE id=?`, runID).Scan(&cur); err != nil {
		return err
	}
	ok := false
	for _, s := range allowed[cur] {
		if s == to {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("%w: %s → %s", ErrTransicionInvalida, cur, to)
	}
	q := `UPDATE runs SET status=? WHERE id=?`
	args := []any{string(to), runID}
	switch to {
	case StateRunning:
		q = `UPDATE runs SET status=?, started_at=? WHERE id=?`
		args = []any{string(to), Now(), runID}
	case StateAwaitingReview, StateFailed, StateFailedAuth, StateFailedQuota,
		StateFailedVerif, StateCancelled, StateSkipped:
		q = `UPDATE runs SET status=?, finished_at=? WHERE id=?`
		args = []any{string(to), Now(), runID}
	}
	if _, err := db.Exec(q, args...); err != nil {
		return err
	}
	db.Audit(runID, "", "system", "state_"+string(to), jsonDetail(detail))
	db.notify()
	return nil
}

func (db *DB) SetRunField(runID, col string, v any) error {
	// Lista blanca: nada de interpolar nombres de columna que vengan de fuera.
	switch col {
	case "worktree_path", "worktree_branch", "base_commit", "provider_session_id",
		"provider_turn_id", "cost_usd", "stop_subtype", "summary", "error_code",
		"exit_code", "quota_reset_at":
	default:
		return fmt.Errorf("columna no permitida: %s", col)
	}
	_, err := db.Exec(`UPDATE runs SET `+col+`=? WHERE id=?`, v, runID)
	if err == nil {
		db.notify()
	}
	return err
}

func (db *DB) GetRun(id string) (*Run, error) {
	r := &Run{}
	var wt, br, bc sql.NullString
	err := db.QueryRow(`SELECT id,task_id,task_revision_id,scheduled_for_utc,status,
		worktree_path,worktree_branch,base_commit,cost_usd,summary,error_code
		FROM runs WHERE id=?`, id).
		Scan(&r.ID, &r.TaskID, &r.RevisionID, &r.ScheduledForUTC, &r.Status,
			&wt, &br, &bc, &r.CostUSD, &r.Summary, &r.ErrorCode)
	if err != nil {
		return nil, err
	}
	r.WorktreePath, r.WorktreeBranch, r.BaseCommit = wt.String, br.String, bc.String
	return r, nil
}

// ---------- cancelación sin canal ----------

func (db *DB) RequestCancel(runID string) error {
	if _, err := db.Exec(`UPDATE runs SET cancel_requested=1 WHERE id=?`, runID); err != nil {
		return err
	}
	db.notify()
	return db.Audit(runID, "", "user", "cancel_requested", `{}`)
}

func (db *DB) CancelRequested(runID string) bool {
	var v int
	if err := db.QueryRow(`SELECT cancel_requested FROM runs WHERE id=?`, runID).Scan(&v); err != nil {
		return false
	}
	return v != 0
}

// ---------- eventos y evidencia ----------

func (db *DB) AppendEvent(runID, eventType, payloadJSON string) error {
	_, err := db.Exec(`INSERT INTO run_events (run_id,sequence,event_type,payload_json,created_at)
		VALUES (?, (SELECT COALESCE(MAX(sequence),0)+1 FROM run_events WHERE run_id=?), ?,?,?)`,
		runID, runID, eventType, payloadJSON, Now())
	return err
}

func (db *DB) Audit(runID, taskID, actor, action, detailJSON string) error {
	var r, t any
	if runID != "" {
		r = runID
	}
	if taskID != "" {
		t = taskID
	}
	_, err := db.Exec(`INSERT INTO audit (run_id,task_id,at,actor,action,detail_json) VALUES (?,?,?,?,?,?)`,
		r, t, Now(), actor, action, jsonDetail(detailJSON))
	return err
}

// ---------- bandeja de la mañana ----------

type InboxItem struct {
	RunID, TaskName, ProjectName string
	Status                       State
	FinishedAt                   sql.NullString
	Summary                      sql.NullString
	CostUSD                      sql.NullFloat64
}

func (db *DB) Inbox() ([]InboxItem, error) {
	rows, err := db.Query(`
		SELECT r.id, t.name, p.name, r.status, r.finished_at, r.summary, r.cost_usd
		FROM runs r
		JOIN tasks t    ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		WHERE r.review_decision IS NULL
		  AND r.status IN ('awaiting_review','failed','failed_quota','failed_auth',
		                   'failed_verification','running')
		ORDER BY (r.status='running') DESC, r.finished_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxItem
	for rows.Next() {
		var it InboxItem
		if err := rows.Scan(&it.RunID, &it.TaskName, &it.ProjectName, &it.Status,
			&it.FinishedAt, &it.Summary, &it.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ---------- auxiliares ----------

func maxOr(v, pordefecto int) int {
	if v <= 0 {
		return pordefecto
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return s
	}
	b, _ := jsonMarshalString(s)
	return `{"detalle":` + b + `}`
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") || strings.Contains(s, "constraint failed")
}
