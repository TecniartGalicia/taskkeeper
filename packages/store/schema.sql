-- TaskKeeper · esquema local
--
-- WAL es imprescindible: sin demonio, cada ejecución es un proceso distinto que
-- escribe brevemente, y la extensión lee al mismo tiempo. busy_timeout evita que
-- un worker falle por contención en vez de esperar su turno.

CREATE TABLE IF NOT EXISTS projects (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  workspace_path TEXT NOT NULL UNIQUE,
  git_root       TEXT NOT NULL,
  default_branch TEXT NOT NULL,
  created_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_refs (
  id                  TEXT PRIMARY KEY,
  provider            TEXT NOT NULL,
  external_session_id TEXT NOT NULL,
  cwd                 TEXT NOT NULL,
  encoded_project_dir TEXT,
  title               TEXT,
  preview             TEXT,
  last_seen_at        TEXT,
  UNIQUE (provider, external_session_id)
);

CREATE TABLE IF NOT EXISTS tasks (
  id                 TEXT PRIMARY KEY,
  name               TEXT NOT NULL,
  project_id         TEXT NOT NULL REFERENCES projects(id),
  agent              TEXT NOT NULL,               -- claude | codex
  enabled            INTEGER NOT NULL DEFAULT 1,
  conversation_mode  TEXT NOT NULL,               -- new | resume | fork
  session_ref_id     TEXT REFERENCES session_refs(id),
  schedule_rule      TEXT NOT NULL,               -- JSON de scheduler.Rule
  timezone           TEXT NOT NULL,               -- IANA
  next_run_at_utc    TEXT,
  misfire_policy     TEXT NOT NULL DEFAULT 'skip',
  permission_profile TEXT NOT NULL,               -- auditoria | cambios_aislados
  timeout_seconds    INTEGER NOT NULL DEFAULT 3600,
  max_budget_usd     REAL,
  daily_budget_usd   REAL,
  -- Identificador del disparador registrado en el sistema operativo, para
  -- poder retirarlo con exactitud y no tocar nada ajeno.
  os_trigger_id      TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

-- Versión inmutable de la configuración. Editar el prompt o los permisos crea
-- una revisión nueva; las ejecuciones antiguas conservan la que usaron.
CREATE TABLE IF NOT EXISTS task_revisions (
  id            TEXT PRIMARY KEY,
  task_id       TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  version       INTEGER NOT NULL,
  prompt_text   TEXT NOT NULL,
  prompt_sha256 TEXT NOT NULL,
  config_json   TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL,
  UNIQUE (task_id, version)
);

CREATE TABLE IF NOT EXISTS runs (
  id                  TEXT PRIMARY KEY,
  task_id             TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  task_revision_id    TEXT NOT NULL REFERENCES task_revisions(id),
  scheduled_for_utc   TEXT NOT NULL,
  idempotency_key     TEXT NOT NULL,
  status              TEXT NOT NULL,
  started_at          TEXT,
  finished_at         TEXT,
  provider_session_id TEXT,
  provider_turn_id    TEXT,
  worktree_path       TEXT,
  worktree_branch     TEXT,
  base_commit         TEXT,
  exit_code           INTEGER,
  cost_usd            REAL,
  stop_subtype        TEXT,
  quota_reset_at      TEXT,
  summary             TEXT,
  error_code          TEXT,
  -- Cancelación sin canal: la extensión levanta la bandera, el worker la sondea.
  cancel_requested    INTEGER NOT NULL DEFAULT 0,
  review_decision     TEXT,
  review_at           TEXT,
  retry_of_run_id     TEXT REFERENCES runs(id)
);

-- Núcleo de la idempotencia: una tarea, un instante, una ejecución.
CREATE UNIQUE INDEX IF NOT EXISTS idx_runs_idempotency ON runs(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_runs_bandeja ON runs(status, finished_at DESC);

CREATE TABLE IF NOT EXISTS run_events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id       TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  sequence     INTEGER NOT NULL,
  event_type   TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  UNIQUE (run_id, sequence)
);

-- Evidencia. Se escribe desde el primer día: añadirla tarde obliga a rediseñar,
-- y es lo único por lo que después se puede cobrar sin recortar el producto.
CREATE TABLE IF NOT EXISTS audit (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id      TEXT REFERENCES runs(id) ON DELETE SET NULL,
  task_id     TEXT,
  at          TEXT NOT NULL,
  actor       TEXT NOT NULL,   -- system | user
  action      TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
