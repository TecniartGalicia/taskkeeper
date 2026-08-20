# TaskKeeper — Plan de construcción auditado

Argalla · Tecniart Galicia, S.L. · 18 de agosto de 2026 · **v2.0**

Este documento **sustituye** a `11-agent-calendar-orchestrator.md` y `11-PLAN-EJECUCION.md` como fuente única para construir. Aquellos quedan como historia del razonamiento.

Cambia porque el análisis de competencia demostró que la promesa original ya la cumplen tres productos gratuitos, uno de ellos del propio fabricante. Ver `ANALISIS-COMPETENCIA.md`.

---

## 1. Qué es TaskKeeper

> **Tus agentes trabajan de noche sin tocar tu repositorio. Por la mañana revisas el diff y decides.**

No es un calendario. Programar tareas es gratis en Claude Code, en Codex y en varias extensiones. Lo que no ofrece nadie es que ese trabajo ocurra **aislado**, quede **revisable** y no pueda romperte nada.

Las seis capacidades que forman el producto, ninguna disponible hoy en ningún competidor:

| | Capacidad | Por qué importa |
|---|---|---|
| 1 | **Worktree aislado por ejecución** | Los demás lanzan el agente sobre tu directorio de trabajo. Si se equivoca de madrugada, se equivoca sobre tu código |
| 2 | **Bandeja de la mañana con diff y aceptar/rechazar** | Nada llega a tu rama sin que lo mires |
| 3 | **Retomar o derivar una conversación** | Los demás siempre arrancan en blanco y pierden el contexto |
| 4 | **Claude y Codex con un solo modelo de seguridad** | Un panel, unos permisos, un historial |
| 5 | **Presupuesto por ejecución y tope diario** | Una tarea nocturna no puede dejarte sin cuota por la mañana |
| 6 | **Permisos que de verdad se aplican** | Medido: por defecto el agente arranca con todos los permisos concedidos |

**El núcleo es gratuito.** Ver §11.

---

## 2. Lo que cambia respecto al plan anterior

Un plan que no dice qué tira a la basura no es auditable.

| Decisión anterior | Ahora | Motivo |
|---|---|---|
| Demonio persistente con su propio reloj | **Sin demonio.** Dispara el programador del sistema | Windows Task Scheduler ya da durabilidad, supervivencia al reinicio y despertar. Reimplementarlo era trabajo y riesgo gratis |
| Named Pipe con SDDL para el canal | **Descartado.** La extensión lee la base local | Sin demonio no hay con quién hablar. El código queda en el repositorio, sin uso, documentado como tal |
| Mutex de instancia única | **Innecesario** para el demonio; se reaprovecha la idea para el tope de concurrencia | No hay proceso único que proteger |
| macOS en la fase 6, tras publicar | **Fase 3**, antes de publicar | Sin demonio, macOS es `launchd` en lugar de Task Scheduler y el resto es portable. Deja de ser caro |
| Suscripción de 8–10 €/mes | **Gratis** el núcleo | Tres competidores gratuitos. Cobrar por programar era una conversación perdida |
| El calendario es la cara del producto | **La bandeja de la mañana** es la cara | El calendario es el mecanismo, no el valor |
| 9–11 semanas | **6–7 semanas** | Consecuencia de todo lo anterior |

**Lo que se conserva de la Fase 0**, ya escrito y probado:

- `packages/scheduler` — cálculo de ocurrencias con zonas y cambios de hora. Sigue siendo necesario para traducir nuestro modelo de calendario a disparadores del sistema y para mostrar las próximas ejecuciones. 6 pruebas.
- `packages/platform/windows/job.go` — Job Object. Sigue siendo necesario: cada ejecución mata su propia jerarquía de procesos. 2 pruebas.
- Todos los hallazgos de `docs/fase-0/RESULTADOS.md`.

Se descarta `ipc.go` y sus 3 pruebas. Se hicieron, funcionan, y la arquitectura correcta ya no los necesita. Queda anotado aquí para que nadie los reviva sin motivo.

---

## 3. Decisiones cerradas

| # | Decisión | Motivo | Se verifica con |
|---|---|---|---|
| D1 | **Worker en Go**, extensión en TypeScript | Binario estático sin tiempo de ejecución instalado; Job Objects nativos; compila para macOS desde la misma máquina | P-01 ✅ |
| D2 | **Sin demonio.** El programador del sistema dispara un worker por ejecución | Durabilidad, reinicio y despertar salen gratis del sistema operativo | P-20 |
| D3 | **Una tarea del sistema por tarea de TaskKeeper**, en su propia carpeta, sin tocar nada ajeno | Permite borrar y editar sin efectos colaterales | P-21 |
| D4 | **Job Object** con `KILL_ON_JOB_CLOSE` por ejecución | `taskkill /T` no alcanza a un nieto huérfano | P-01 ✅ |
| D5 | **SQLite en modo WAL**, escritores múltiples breves, lectores concurrentes | Sin demonio no hay escritor único; WAL lo admite | P-22 |
| D6 | **Turnos por ficheros de bloqueo**, N ficheros = N ejecuciones simultáneas | Portable a macOS sin API específica; sin proceso coordinador | P-23 |
| D7 | **Cancelación por bandera en la base**, sondeada por el worker | Sin canal no hace falta canal | P-24 |
| D8 | Descubrimiento de sesiones: Claude en la extensión, Codex en el worker | El SDK de Claude solo existe en TypeScript; Codex habla un protocolo | P-03 ✅ |
| D9 | **Detección de agentes en cada preflight**, por patrón, nunca por PATH | Medido: ninguno está en el PATH y la ruta caduca al actualizar | P-00 ✅ |
| D10 | **Neutralizar la configuración ambiente** siempre | Medido: por defecto arranca en `bypassPermissions` con 16 permisos | P-06 ✅ |
| D11 | **Cortar al primer 401**, no esperar los diez reintentos | Medido: una credencial caducada tarda minutos en rendirse | P-06 ✅ |
| D12 | Codex por **App Server**, con `codex exec` de respaldo | `thread/fork` y `turn/interrupt` solo existen ahí | P-19 |
| D13 | **Núcleo gratuito**, equipos de pago más adelante | Tres competidores gratuitos | Decisión de producto |

---

## 4. Arquitectura sin demonio

```text
                 Programador del sistema
        (Task Scheduler en Windows · launchd en macOS)
                          │  dispara a su hora, despierta el equipo,
                          │  sobrevive al reinicio
                          ▼
                 taskkeeper-worker.exe --run <task-id>
                          │
        ┌─────────────────┼──────────────────┐
        ▼                 ▼                  ▼
   turno libre?      worktree Git      Job Object
   (fichero de       aislado desde     mata toda la
    bloqueo)         el commit base    descendencia
                          │
                          ▼
                Adaptador Claude / Codex
                          │
                          ▼
                 SQLite (WAL) ── eventos, estado, coste
                          │
                          ▼
              Extensión de VS Code (solo lectura)
              bandeja · diff · aceptar / rechazar
```

Tres ejecutables, ninguno residente:

| Pieza | Cuándo corre | Qué hace |
|---|---|---|
| `taskkeeper-worker` | Lo lanza el programador del sistema, o «Ejecutar ahora» | Una ejecución de principio a fin |
| `taskkeeper-ctl` | Lo lanza la extensión | Alta, baja y edición de tareas; registra y retira disparadores |
| Extensión | Con VS Code abierto | Interfaz. Lee la base; nunca lanza agentes |

**Lo que se gana:** no hay proceso que pueda estar muerto cuando llegue la hora. Es el fallo que hundiría el producto y ahora no puede ocurrir.

**Lo que se pierde:** no hay un coordinador central. Se resuelve con el fichero de turno (§6.2) y con la clave de idempotencia, que ya estaba.

---

## 5. Modelo de datos

Cambios respecto al anterior, en negrita:

```sql
PRAGMA journal_mode = WAL;          -- imprescindible: varios escritores breves
PRAGMA busy_timeout = 5000;         -- un worker no debe fallar por contención
PRAGMA foreign_keys = ON;

CREATE TABLE tasks (
  id                  TEXT PRIMARY KEY,
  name                TEXT NOT NULL,
  project_id          TEXT NOT NULL REFERENCES projects(id),
  agent               TEXT NOT NULL,              -- claude | codex
  enabled             INTEGER NOT NULL DEFAULT 1,
  conversation_mode   TEXT NOT NULL,              -- new | resume | fork
  session_ref_id      TEXT REFERENCES session_refs(id),
  schedule_rule       TEXT NOT NULL,              -- JSON: once|daily|weekly
  timezone            TEXT NOT NULL,              -- IANA
  next_run_at_utc     TEXT,
  misfire_policy      TEXT NOT NULL,
  permission_profile  TEXT NOT NULL,
  timeout_seconds     INTEGER NOT NULL,
  max_budget_usd      REAL,
  daily_budget_usd    REAL,
  -- Identificador del disparador registrado en el sistema, para poder
  -- retirarlo exactamente y no tocar nada más.
  os_trigger_id       TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE TABLE runs (
  id                  TEXT PRIMARY KEY,
  task_id             TEXT NOT NULL REFERENCES tasks(id),
  task_revision_id    TEXT NOT NULL REFERENCES task_revisions(id),
  scheduled_for_utc   TEXT NOT NULL,
  idempotency_key     TEXT NOT NULL,
  status              TEXT NOT NULL,
  started_at          TEXT,
  finished_at         TEXT,
  provider_session_id TEXT,
  provider_turn_id    TEXT,                       -- necesario para turn/interrupt
  worktree_path       TEXT,
  worktree_branch     TEXT,
  base_commit         TEXT,
  exit_code           INTEGER,
  cost_usd            REAL,
  stop_subtype        TEXT,
  quota_reset_at      TEXT,
  summary             TEXT,
  error_code          TEXT,
  -- Cancelación sin canal: la extensión pone la bandera, el worker la sondea.
  cancel_requested    INTEGER NOT NULL DEFAULT 0,
  -- Revisión humana.
  review_decision     TEXT,                       -- accepted | rejected | null
  review_at           TEXT,
  retry_of_run_id     TEXT REFERENCES runs(id)
);

CREATE UNIQUE INDEX idx_runs_idempotency ON runs(idempotency_key);
CREATE INDEX idx_runs_bandeja ON runs(status, finished_at DESC);

-- Evidencia (recomendación 7). Se escribe desde el primer día porque
-- añadirla tarde obliga a rediseñar.
CREATE TABLE audit (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id        TEXT REFERENCES runs(id),
  at            TEXT NOT NULL,
  actor         TEXT NOT NULL,        -- system | user
  action        TEXT NOT NULL,        -- created | started | accepted | rejected | cancelled
  detail_json   TEXT NOT NULL         -- perfil, permisos efectivos, commit, coste
);
```

---

## 6. La ejecución

### 6.1 Ciclo completo del worker

```go
// taskkeeper-worker --run <task-id> [--occurrence <rfc3339>]
func Run(ctx context.Context, taskID string, occurrence time.Time) error {
	db := store.MustOpen(cfg.DBPath)
	defer db.Close()

	task, rev := store.LoadTaskAndRevision(db, taskID)

	// 1. Idempotencia. El índice único es la barrera real: si dos disparos
	//    coinciden (recuperación tras reinicio más disparo normal), el segundo
	//    insert falla y ese proceso se retira sin hacer nada.
	run, created := store.CreateRunIfAbsent(db, task, rev, occurrence)
	if !created {
		return nil
	}

	// 2. Turno. Sin coordinador central, el cupo global se reparte con
	//    ficheros de bloqueo (§6.2).
	slot, err := turns.Acquire(cfg.MaxConcurrent, cfg.SlotDir, 30*time.Minute)
	if err != nil {
		store.Transition(db, run, StateSkipped, "sin turno libre")
		return nil
	}
	defer slot.Release()

	// 3. Preflight. Cualquier fallo aquí es seguro: no se ha lanzado nada.
	if err := preflight(ctx, db, run, task); err != nil {
		store.Transition(db, run, classifyPreflight(err), err.Error())
		return nil
	}

	// 4. Worktree desde el commit resuelto, no desde la rama: un push ajeno
	//    entre medias no debe cambiar la base.
	wt, err := git.CreateWorktree(ctx, task.Project, run)
	if err != nil {
		store.Transition(db, run, StateFailed, err.Error())
		return nil
	}

	// 5. El agente, dentro de su Job Object.
	job, _ := platform.NewProcessGroup()
	defer job.Close() // KILL_ON_JOB_CLOSE: red de seguridad si el worker muere

	cmd, err := adapters.For(task.Agent).Command(buildRequest(task, rev, wt))
	if err != nil {
		store.Transition(db, run, StateFailed, err.Error())
		return nil
	}
	job.Prepare(cmd)
	if err := cmd.Start(); err != nil { ... }
	job.Adopt(cmd.Process.Pid)

	store.Transition(db, run, StateRunning, "")

	// 6. Consumir el flujo, clasificando y vigilando la cancelación.
	outcome := consume(ctx, db, run, cmd, job, task.TimeoutSeconds)

	// 7. Verificaciones y diff, si el agente terminó bien.
	if outcome.State == StateVerifying {
		outcome = verify(ctx, db, run, wt, rev.Checks)
	}

	store.Finish(db, run, outcome)
	audit.Write(db, run, "system", string(outcome.State), outcome.Detail)
	return nil
}
```

### 6.2 Turnos sin coordinador

El plan anterior confiaba el cupo global al demonio. Sin demonio hace falta algo que funcione entre procesos y en las dos plataformas. La solución más simple que cumple: **N ficheros, N turnos.**

```go
// Acquire intenta hacerse con uno de los N ficheros de turno en exclusiva.
// Portable: en Windows la exclusividad la da el modo de apertura; en macOS,
// flock. No hace falta ningún proceso coordinador.
func Acquire(n int, dir string, wait time.Duration) (*Slot, error) {
	deadline := time.Now().Add(wait)
	for {
		for i := 0; i < n; i++ {
			p := filepath.Join(dir, fmt.Sprintf("turno-%d.lock", i))
			if f, ok := tryExclusive(p); ok {
				return &Slot{f: f}, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, ErrSinTurno
		}
		time.Sleep(5 * time.Second)
	}
}
```

Un worker que muere sin liberar deja de mantener el bloqueo porque el sistema operativo cierra el descriptor: **no quedan turnos huérfanos**, que era el problema clásico de esta técnica con ficheros marcador.

### 6.3 Cancelación y clasificación

```go
func consume(ctx context.Context, db *sql.DB, run *Run, cmd *exec.Cmd,
             job platform.ProcessGroup, timeout int) Outcome {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024) // líneas JSON largas
	deadline := time.After(time.Duration(timeout) * time.Second)
	tick := time.NewTicker(3 * time.Second) // sondeo de cancelación
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			if store.CancelRequested(db, run.ID) {
				job.KillAll()
				return Outcome{State: StateCancelled}
			}
		case <-deadline:
			job.KillAll()
			return Outcome{State: StateFailed, Code: "timeout"}
		default:
		}

		if !sc.Scan() { break }
		ev, ok := adapter.ParseLine(sc.Bytes())
		if !ok { continue }
		store.AppendEvent(db, run, ev)

		// Medido en Fase 0: una credencial caducada NO falla, se reintenta
		// diez veces con espera creciente. Se corta al primero.
		if ev.Type == EventAPIRetry {
			switch ev.ErrorStatus {
			case 401:
				job.KillAll()
				return Outcome{State: StateFailedAuth, Code: ev.ErrorCode}
			case 429:
				job.KillAll()
				return Outcome{State: StateFailedQuota, RetryAt: ev.ResetAt}
			}
		}
	}
	return classifyTerminal(cmd.Wait(), lastEvent)
}
```

---

## 7. Registro en el programador del sistema

Nuestro modelo de calendario se traduce a un disparador nativo. `packages/scheduler` sigue calculando ocurrencias para mostrarlas y para las reglas que el sistema no sabe expresar.

**Windows.** Una tarea por tarea de TaskKeeper, bajo la carpeta `Argalla\TaskKeeper`. Del hallazgo de la Fase 0: el XML **necesita `<UserId>` real** o falla con un engañoso «Acceso denegado».

```go
func Register(t *Task) error {
	uid, err := platform.CurrentUserPrincipal() // "ARGALLA\\kirne", nunca %USERNAME%
	if err != nil { return err }
	xml := renderTaskXML(taskXMLParams{
		UserID:     uid,
		Trigger:    triggerFor(t.ScheduleRule, t.Timezone),
		WakeToRun:  true,
		Command:    cfg.WorkerPath,
		Arguments:  "--run " + t.ID,
		Instances:  "IgnoreNew", // el propio sistema evita el solape
	})
	return schtasks.CreateFromXML(`Argalla\TaskKeeper\`+t.ID, xml)
}
```

Reglas de convivencia, copiadas de lo que la competencia hace bien:

- Solo se tocan las tareas de nuestra carpeta. Nada más del Programador se modifica.
- Editar una tarea regenera su disparador; borrarla lo retira.
- El preflight avisa si los temporizadores de reactivación no permiten despertar: **tri-estado**, y el valor «solo temporizadores importantes» tampoco sirve.

**macOS.** Un agente de `launchd` por tarea, con `StartCalendarInterval`. Ejecuta al despertar las ocurrencias perdidas, pero no despierta el Mac: se dice en el asistente, no en la letra pequeña.

---

## 8. La bandeja de la mañana

Es la cara del producto y la pantalla de la demostración.

```
┌─ TASKKEEPER ─────────────────────────────────────────────┐
│  ANOCHE                                                  │
│                                                          │
│  ● Mantenimiento semanal · besbello-web        03:15     │
│    12 archivos · tests 48/48 ✓ · 0,42 €                  │
│    «Actualizadas 7 dependencias menores…»                │
│    [ Ver diff ]     [ Aceptar ]  [ Rechazar ]            │
│                                                          │
│  ▲ Revisión de PR · alphawolf-ios              04:00     │
│    Cuota agotada · reintento a las 09:00                 │
│                                                          │
│  ○ Auditoría diaria · ponktio               en curso ⟳   │
└──────────────────────────────────────────────────────────┘
```

Consulta que la alimenta:

```sql
SELECT r.*, t.name, p.name AS proyecto
FROM runs r
JOIN tasks t ON t.id = r.task_id
JOIN projects p ON p.id = t.project_id
WHERE r.review_decision IS NULL
  AND r.status IN ('awaiting_review','failed','failed_quota',
                   'failed_auth','failed_verification','running')
ORDER BY r.finished_at DESC NULLS FIRST;
```

**Aceptar** funde la rama del worktree en la rama base del checkout principal, **en local y nunca con push**, y limpia el worktree. **Rechazar** borra worktree y rama sin dejar rastro. Las dos acciones escriben en `audit`.

El diff se abre con el visor nativo de VS Code (`vscode.diff`). No se construye un visor propio.

La extensión detecta cambios observando el fichero de la base y una marca que el worker toca al terminar. Latencia de menos de un segundo sin sondeo activo.

---

## 9. Seguridad

Los perfiles, ya corregidos con lo medido en la Fase 0:

| Perfil | Claude Code | Codex |
|---|---|---|
| **Auditoría** | `--permission-mode default --settings <perfil> --allowedTools "Read" "Glob" "Grep" --disallowedTools "Edit" "Write" "Bash"` | `-s read-only --ignore-user-config` |
| **Cambios aislados** | `--permission-mode acceptEdits --settings <perfil> --add-dir <worktree>` con patrones acotados | `-s workspace-write --ignore-user-config --cd <worktree>` |

Reglas que el código hace cumplir, no solo el documento:

1. **`--settings` e `--ignore-user-config` son obligatorios.** Medido: sin ellos el agente arranca con `bypassPermissions` y 16 permisos concedidos por la configuración del usuario.
2. Modos prohibidos por **modo**, no por alias: `bypassPermissions`, `auto`, `dontAsk`. En Codex, nunca `--dangerously-bypass-approvals-and-sandbox`.
3. Argumentos como matriz, jamás como cadena de shell.
4. Redacción de secretos en el **único** escritor de eventos, para que ningún camino la evite.
5. El preflight comprueba que el espacio de trabajo es **de confianza**; si no, la tarea de escritura no se programa.

---

## 10. Multiplataforma

Una interfaz, dos implementaciones, ambas en el lanzamiento:

| Necesidad | Windows | macOS |
|---|---|---|
| Disparo | Task Scheduler, `WakeToRun` | `launchd`, `StartCalendarInterval` |
| Matar la jerarquía | Job Object `KILL_ON_JOB_CLOSE` | Grupo de procesos, `kill(-pgid)` |
| Turnos | Ficheros de bloqueo | Ficheros de bloqueo (`flock`) |
| Despertar el equipo | **Sí**, si la energía lo permite | **No**: ejecuta al despertar |

La última fila es la única diferencia visible para el usuario y se declara en el asistente y en la ficha de tienda.

---

## 11. Gratis

**Núcleo gratuito y sin recortes**, para siempre: los dos agentes, tareas ilimitadas, worktrees, bandeja de revisión, historial completo y control de gasto. Sin límite de tres tareas, sin historial de siete días, sin funciones capadas.

Motivo: hay tres alternativas gratuitas. Cobrar por programar era perder la conversación antes de empezarla. Y el problema real hoy no es monetizar —las tres extensiones publicadas de Argalla suman cinco instalaciones— sino que alguien las use.

**Lo que se cobrará más adelante**, cuando haya usuarios y solo sobre lo que una persona sola no necesita:

- Políticas de permisos compartidas por equipo.
- Plantillas de tarea de la organización.
- Auditoría central y exportable, que es el motivo por el que la tabla `audit` se escribe desde hoy.
- Panel de gasto por proyecto y por cliente.

**Consecuencia en el plan:** la licencia de Polar sale del camino crítico. La Fase 5 se reduce a publicar.

---

## 12. Fases

| Fase | Duración | Entregable | Criterio de salida |
|---|---|---|---|
| **0. Prueba técnica** | ✅ **Hecha** | 9 puertas resueltas, scheduler y Job Object escritos | Cerrada |
| **1. Ejecución** | 2 sem | Una tarea real, disparada por el sistema, en su worktree | P-20…P-24 en verde; el checkout principal intacto |
| **2. Bandeja** | 2 sem | La pantalla de la mañana, con diff y aceptar/rechazar | Aceptar funde y limpia; rechazar no deja rastro |
| **3. Seguridad y macOS** | 1,5 sem | Perfiles aplicados y el mismo VSIX en un Mac | Ningún secreto en registros; suite verde en las dos plataformas |
| **4. Beta** | 1 sem | Cinco personas de fuera instalándolo | Cinco instalaciones limpias sin ayuda |
| **5. Publicación** | 0,5 sem | Marketplace y Open VSX, gratis | Ficha con las limitaciones declaradas |

**6–7 semanas de trabajo efectivo.** La validación con usuarios sigue siendo puerta de entrada a la Fase 1.

Orden de recorte si hiciera falta: primero los flujos encadenados, después la gestión de las tareas nativas de Claude, después Codex. **Nunca** el worktree ni la bandeja: son el producto.

---

## 13. Pruebas

| Id | Comprueba | Cierra |
|---|---|---|
| P-01 ✅ | El Job Object mata al nieto huérfano | D4 |
| P-03 ✅ | `listSessions` devuelve sesiones reales | D8 |
| P-05 ✅ | Cambios de hora y zonas, 6 casos | Calendario |
| P-06 ✅ | Cuota y credencial se distinguen; se corta al primer 401 | D10, D11 |
| P-20 | Una tarea registrada se dispara sola con VS Code cerrado | D2 |
| P-21 | Editar y borrar una tarea no toca ninguna otra del sistema | D3 |
| P-22 | Dos workers simultáneos escriben sin corromper la base | D5 |
| P-23 | Con un turno, dos ejecuciones se serializan; un worker muerto libera el suyo | D6 |
| P-24 | La bandera de cancelación detiene el proceso y su descendencia | D7 |
| P-25 | Aceptar funde en la rama base sin hacer push; rechazar no deja rastro | Bandeja |
| P-26 | Ningún secreto inyectado aparece en base, registros ni avisos | Seguridad |
| P-27 | El agente no puede escribir fuera de su worktree | Producto |
| P-19 | `thread/fork` de Codex crea un hilo nuevo sin alterar el original | D12 |
| P-28 | La misma suite pasa en macOS | Multiplataforma |

---

## 14. Auditoría del plan

| Riesgo | Control | Prueba |
|---|---|---|
| El disparador no salta a su hora | Lo registra el sistema operativo, no un proceso nuestro | P-20 |
| Procesos huérfanos tras cancelar | Job Object con cierre por destrucción | P-01 ✅ |
| Dos ejecuciones a la vez corrompen la base | WAL más `busy_timeout`, escrituras breves | P-22 |
| Un worker muerto bloquea el cupo para siempre | El bloqueo lo suelta el sistema al cerrar el descriptor | P-23 |
| Credencial caducada consume el tiempo límite | Corte al primer 401 | P-06 ✅ |
| El agente arranca con más permisos de los previstos | `--settings` e `--ignore-user-config` obligatorios | P-26, P-27 |
| Un agente toca el repositorio principal | Worktree desde el commit resuelto, `--add-dir` acotado | P-27 |
| Aceptar por error mete cambios malos | El diff se revisa antes; nunca hay push automático | P-25 |
| Ensuciar el Programador del sistema | Carpeta propia, se toca solo lo creado | P-21 |
| El fabricante mejora su programación nativa | El valor no es programar: es el aislamiento y la revisión | Posicionamiento |
| Nadie quiere el producto | Validación previa, y con el producto gratis la señal es la retención a la segunda semana | Entrevistas |

### Puntos débiles que este plan reconoce

1. **Sin demonio no hay coordinación fina.** El cupo por ficheros de turno es correcto pero tosco: no hay prioridades ni cola ordenada. Si eso hiciera falta, vuelve el demonio, y sería un cambio grande.
2. **La latencia de la interfaz depende de observar un fichero.** Cumple el segundo exigido, pero no es un canal de verdad. Si el uso pide más, hay que reabrir la decisión.
3. **En macOS el equipo no se despierta.** Se declara; no se disimula.
4. **`codex app-server` es experimental y va por una alfa.** La degradación a `codex exec` hay que ejercitarla en las pruebas, no darla por hecha.
5. **Superior no es lo mismo que querido.** El producto será mejor que todo lo que hay. Eso no demuestra que exista demanda.

---

## 15. Fuera de alcance

Runner remoto, Linux, aplicaciones móviles, equipos y SSO, aprobaciones remotas, comparación de agentes con el mismo prompt y push o despliegue automáticos. Nada de las decisiones anteriores lo bloquea.


---

## 20. Ajustes tras la Fase 1

Tres correcciones aplicadas al terminar la Fase 1, todas por cosas que estaban en el sitio equivocado.

### 20.1 «Ejecutar ahora» no puede bloquear

Lanzaba el worker y esperaba a que terminase, así que el botón dejaba la extensión colgada todo lo que durase el agente. Ahora se lanza desatendido y devuelve el control en decenas de milisegundos; el progreso se sigue por la bandeja, igual que en una ejecución nocturna.

Se descartó hacerlo con el disparo del programador (`schtasks /Run`), que era la primera idea: por ahí el worker llegaría con los argumentos registrados y derivaría la hora prevista, que es lo contrario de lo que quiere decir «ahora». Un arranque manual usa el instante actual a propósito, para que probar a mano no consuma la ocurrencia de esta noche.

### 20.2 El cupo de simultáneas vive en la base

Estaba como argumento en la línea de órdenes de cada disparador, así que cambiarlo de uno a dos obligaba a reescribir todos los disparadores registrados: un ajuste, N modificaciones, y la posibilidad de dejar tareas con un cupo distinto de las demás. Ahora es un ajuste de la máquina guardado en `meta`, que cada worker consulta al arrancar. La bandera `--cupo` se queda solo como sobrescritura para diagnóstico.

### 20.3 La ocurrencia se deriva; las dos políticas de retraso dejan de pelearse

Este arrastraba un fallo real. El worker usaba «ahora» como hora de la ocurrencia, con dos consecuencias:

- **No podía saber si llegaba tarde**, así que la política elegida por el usuario nunca se leía. El XML del disparador lleva `StartWhenAvailable`, es decir, la política de Windows —ejecutar al encender, siempre y sin límite—, y esa ganaba en silencio.
- **La protección contra duplicados no protegía.** Funciona por «esta tarea, a esta hora, una sola vez»; si la hora es siempre «ahora», nunca coincide con nada. Estaba probada pasando la hora a mano, pero en una ejecución real no hacía su trabajo.

Ahora el worker **deriva de qué hora viene** con `scheduler.Previous`, a partir de la regla y la zona de la tarea. De ahí salen las tres cosas:

| | Antes | Ahora |
|---|---|---|
| Saber el retraso | Imposible | `ahora − hora prevista` |
| Política del usuario | Ignorada | Se aplica: omitir, ejecutar si el retraso cabe, o dejar pendiente |
| Duplicados | Sin protección real | Disparo normal y recuperación son la misma ocurrencia |

Y el reparto con el sistema operativo queda explícito: **Windows nos despierta aunque sea tarde, y nosotros decidimos si merece la pena ejecutar.** Cada uno hace lo que sabe hacer.

Una omisión también deja constancia, con el retraso escrito: que no pasara nada es información, y sin ella el usuario no entiende el silencio de la mañana.

**Efecto secundario en las pruebas:** P-20 empezó a fallar al aplicar esto, porque su tarea era diaria a las 03:00 y a cualquier otra hora del día la ejecución llega tarde y se omite. Se corrigió la prueba, no el código: ahora fija `run_if_late` con una ventana amplia, porque lo que P-20 mide es el disparo y la política tiene sus propias pruebas.


---

## 21. Estado tras la Fase 2

Fase 2 (la bandeja de la mañana y la extensión) **completa y auditada con agentes reales**.

### Lo construido

- **Extensión de VS Code** (`apps/vscode-extension`): vistas Anoche/Tareas/Historial, asistente de siete pasos, diff con el editor nativo, aceptar/rechazar/archivar/cancelar/ejecutar-ahora/pausar/reanudar/editar-prompt, detalle de ejecución, ajuste de cupo. Sin módulos nativos: todo pasa por `taskkeeper-ctl --json`. Actualización en vivo observando `cambios.marca`. 148 cadenas en inglés y castellano.
- **Contrato ctl↔extensión**: `ctl` gana salida `--json` estable y las órdenes que la interfaz consume. Es la única frontera.
- **Selector de sesiones de Claude** sin redistribuir el Agent SDK (licencia «all rights reserved»): solo lo que Anthropic documenta —nombre de fichero y mtime—, con título de cortesía y degradación a id+fecha.
- **Binarios en carpeta estable** (`%LOCALAPPDATA%`), no en la de la extensión, porque los disparadores del sistema apuntan al worker por ruta absoluta y la de la extensión cambia en cada actualización.

### Auditoría con agentes reales

| Prueba real | Resultado |
|---|---|
| Claude, perfil auditoría | Responde en dos frases; 0,20 USD; a la bandeja |
| Claude, cambios aislados | Edita un fichero en su worktree; **aceptar funde en `main` sin push** y limpia; el checkout principal intacto todo el rato |
| Codex, cuota agotada | El error llega como **texto sin código HTTP** |
| Tope de presupuesto | `--max-budget-usd` corta la ejecución en `error_max_budget_usd` |

### Hallazgos convertidos en código

1. **Codex clasifica cuota y credencial por patrón de texto** (Claude no lo necesita, Codex sí) y **extrae la hora de reinicio** del mensaje «try again at …».
2. **Un reintento por cuota**, programado como disparador puntual del sistema a esa hora; no se encadena un segundo en 24 h. Sin demonio, el «espera y reintenta» solo puede vivir en el programador.
3. **`archivar`** para sacar de la bandeja lo fallido o lo que terminó sin cambios (una auditoría), distinto de aceptar/rechazar.
4. **`ListTasks` se bloqueaba** con más de una tarea (consulta anidada en un cursor con una sola conexión) — lo destapó la prueba de humo, con regresión añadida.
5. **P-20 es flaky bajo carga**: el servicio de tareas encola y su latencia va de 3 a 40 s; margen subido a 120 s.

### Números

- **Go: 58 pruebas**, incluidas 3 de integración con el Programador real y las ejecuciones reales fuera de la suite.
- **Extensión: 19 unitarias + 4 de integración** en un VS Code real.
- **VSIX win32-x64: 6,4 MB**, instalado y activado como cliente.

### Deuda anotada

- **Captura de tienda pendiente**: la captura automática de ventana se descartó porque en una máquina en uso puede capturar contenido ajeno. El *harness* de demo queda listo para una máquina limpia; el lanzamiento v0.1.0 es *preview* y puede salir sin captura.
- **`revisado en Fase 3`**: firmar el worker (la guía lo recomienda), redacción de secretos en la vista de eventos (ya existe en el worker; falta comprobar que la extensión no muestre nada crudo).

## 22. Estado tras la Fase 3 (seguridad + macOS)

Fecha: 2026-08-19. Auditoría cerrada.

### Seguridad

- **Redacción de secretos en un único punto**: `packages/redact` borra claves conocidas (Anthropic `sk-ant-`, OpenAI `sk-`, GitHub PAT clásico y de grano fino, Slack, AWS `AKIA`, Google `ya29.`, JWT, PEM y genéricos `bearer`/`api_key=…`) y se aplica **solo** en el escritor de eventos de la base (`store.AppendEvent`) y en los campos `summary`/`error_code` de `SetRunField`. Ningún camino de escritura puede saltárselo.
- **Verificado de extremo a extremo con un agente real**: se lanzó una tarea cuyo *prompt* ordenaba a Claude escribir `API_KEY=sk-ant-…` en un fichero del worktree. Resultado en la base: **0 apariciones del secreto crudo**, 4 eventos con marca `[redactado]`. La aceptación del cambio en el repo es decisión humana; el secreto redactado nunca sale de la base ni llega a la vista.
- **Preserva la forma del JSON**: la sustitución mantiene el prefijo capturado (`api_key: [redactado]`) para que un *payload* siga siendo JSON válido. 12 casos de prueba, incluidos textos limpios que no debe tocar.

### macOS (código que cruza-compila; VSIX aún no)

- `packages/platform/darwin/group.go` — grupo de procesos con `Setpgid` y `kill(-pgid, SIGKILL)`, espejo del Job Object de Windows; `Alive` con `kill(pid, 0)`.
- `packages/platform/darwin/launchd.go` — agentes de usuario en `~/Library/LaunchAgents` bajo el prefijo `com.argalla.taskkeeper.`, con `StartCalendarInterval` (once/daily/weekly, mapeo ISO→launchd del domingo), carga idempotente con `bootout`+`bootstrap`, reintento por cuota como agente puntual.
- `packages/platform/{platform,tareas}_darwin.go` y `packages/runner/vivo_darwin.go` conectan la capa común.
- **`GOOS=darwin GOARCH={amd64,arm64} go build ./...` compila**; binarios Mach-O generados y verificados con `file`. `go vet` de los paquetes portables limpio para darwin.
- **Verdad declarada al usuario**: en macOS el equipo **no se despierta** para ejecutar (launchd ejecuta la ocurrencia perdida al volver del reposo). Es la asimetría con Windows (`RTCWAKE`), recogida en `AvisoReactivacion()`.
- **Por qué el VSIX v0.1.0 sigue siendo solo win32-x64**: el código cruza-compila, pero honestidad de producto = no publicar el *target* darwin sin verificación en hardware real (matar el árbol de procesos, el disparo de launchd y el reintento por cuota se prueban en un Mac, no en cross-compilación). El *target* macOS sale cuando eso se verifique.

### Deuda que sale de la Fase 3 sin cerrar

- **Firma del worker**: la guía la recomienda para que Windows SmartScreen y Gatekeeper de macOS no marquen el binario. Requiere un certificado de firma de código (Authenticode / Apple Developer ID) que **no está disponible**. Queda anotado para el usuario: sin certificado, el worker se distribuye sin firmar y el primer arranque puede mostrar aviso de SmartScreen. No bloquea el *preview* v0.1.0 (el worker se instala desde el VSIX, no se descarga suelto), pero conviene resolverlo antes de la 1.0.

### Números tras la Fase 3

- **Go**: toda la suite pasa (adapters, gitwt, platform, platform/windows, redact, runner, scheduler, store, turns, integración). Cross-compila darwin/amd64 y darwin/arm64.
- **Extensión**: 19 unitarias + 4 de integración en VS Code real.

## 23. Estado tras la Fase 5 (PUBLICADO)

Fecha: 2026-08-19.

### Publicación hecha

- **Repositorio**: https://github.com/TecniartGalicia/taskkeeper (público, MIT), monorepo Go + extensión.
- **CI** (`ci.yml`): ubuntu/windows/macOS. Go build+test en las tres; `npm run check` en las tres; **integración solo en Windows** (única plataforma que se distribuye y donde el worker registra tareas reales); Linux compila el worker cruzado y empaqueta el VSIX win32-x64 como humo.
- **Release** (`release.yml`) por tag `vX.Y.Z`: verifica tag en main = package.json, `go test`, `npm run check`, compila el worker win32-x64, empaqueta y **publica en Marketplace + Open VSX**, adjunta el VSIX a una release de GitHub. Reejecutable (salta lo ya publicado).
- **v0.1.0 publicada en las tres**:
  - VS Code Marketplace: `argalla.taskkeeper` 0.1.0 (win32-x64) — verificado con `vsce show`.
  - Open VSX: `argalla.taskkeeper` 0.1.0@win32-x64 — `ovsx` confirmó «Published» (indexación de la API tarda unos minutos).
  - GitHub release `v0.1.0` con `taskkeeper-v0.1.0-win32-x64.vsix` adjunto (6,74 MB).

### Fallos de CI resueltos en el camino (auditoría entre fases)

1. **El paquete `platform` no compilaba en Linux** (solo `//go:build windows|darwin`). → stub `platform_other.go` + `runner/vivo_other.go` con `//go:build !windows && !darwin`; el resto de la suite ya corre en los runners Linux.
2. **`TestEscritoresConcurrentesEntreProcesos` daba `SQLITE_BUSY`** en el disco del runner de Windows. → `busy_timeout` 5→15 s y `synchronous(NORMAL)` en WAL.
3. **Los tests de ciclo de vida del runner fallaban en Linux** (usan grupos de procesos y `ping -n`). → `//go:build windows || darwin`; se ejecutan en Windows y macOS.

### Lo que queda en manos del usuario (no lo puede hacer el agente)

- **Beta con usuarios externos** (Fase 4, la parte humana): instalar el VSIX en 5 equipos limpios de terceros y recoger fallos antes de quitar el flag `preview`.
- **Firma de código del worker**: requiere certificado Authenticode (Windows) / Apple Developer ID (macOS) que no está disponible; sin él, SmartScreen puede avisar en el primer arranque. No bloquea el *preview*.
- **`target` macOS**: el código cruza-compila; sale como paquete de plataforma propio cuando se verifique en hardware real (matar árbol de procesos, disparo de launchd, reintento por cuota).
- **Redes / anuncio**: X, dev.to, HN, etc. (yo redacto; publica el humano).

## 24. Plan auditado — Panel visual de tarea (v0.2.0)

Fecha: 2026-08-19. Sustituir los 7 diálogos por un panel Webview, en fases, auditando entre cada una. Regla de oro: **el panel NO reimplementa lógica**; llama al mismo `ctl crear/editar/tarea` y a un nuevo `ctl previsualizar` para la matemática de fechas (que sigue viviendo solo en Go, `packages/scheduler`).

### F1 — Panel de creación (Webview)
- `src/ui/taskPanel.ts`: `vscode.window.createWebviewPanel`, CSP con nonce, tema por variables `--vscode-*`. HTML/CSS/JS embebidos (sin recursos externos, la CSP los bloquea).
- Mensajería extensión↔webview: `ready`→datos iniciales (agentes, carpetas del workspace, zona por defecto, plantilla de prompt); `browseRepo`→diálogo de carpeta; `listSessions`→sesiones de esa carpeta; `submit`→`ctl.create` (+ `runNow`). Cadenas l10n (ES/EN) pasadas al webview.
- `taskkeeper.newTask` abre el panel; el asistente actual queda como `taskkeeper.newTaskQuick` (accesible, teclado).
- Reutiliza `createArgs`/`CreateParams` de `ctl.ts` sin cambios.
- **Auditoría F1**: typecheck+lint+unit; un test unitario del generador de HTML (nonce presente, CSP presente, sin `http`); un test del reductor de mensajes→CreateParams; `ctl crear` real desde los mismos params; verificar que el asistente rápido sigue vivo.

### F2 — Varias horas por tarea + «próxima ejecución» en vivo
- `packages/scheduler`: `Rule.Times []string` (retrocompat: si `Times` vacío, se usa `Time`). `Next`/`Previous` iteran todas las horas y eligen la primera/última que cumpla el `After`/`!After`. Nuevas pruebas DST y multi-hora.
- `packages/platform` (Windows + darwin): `EspecDisparador.Horas []time.Time` y varios `<CalendarTrigger>`/`StartCalendarInterval`. Registrar N disparadores en una sola tarea del SO.
- `apps/ctl`: `--hora` acepta lista `15:00,20:00`; `construirRegla`/`registrar` propagan. Nuevo `ctl previsualizar --regla --hora --dias --zona` → próximas 3 ocurrencias (locales) SIN crear tarea.
- Panel: chips de hora (añadir/quitar) + línea «Próxima ejecución» que llama a `previsualizar` (debounce) por la extensión.
- **Auditoría F2**: pruebas del scheduler (multi-hora Next/Previous, orden, DST); test de integración Windows que una tarea de dos horas registra dos triggers y dispara; `ctl previsualizar` real; `ctl crear --hora 15:00,20:00` real y comprobar los dos triggers en `schtasks /Query`.

### F3 — Editar en el mismo panel
- El panel acepta un `taskId`: precarga vía `ctl.task(id)` (nombre, agente, prompt, regla, permisos, misfire, presupuesto, sesión) y guarda con `ctl.edit`. Validación en el sitio.
- `taskkeeper.editTask` abre el panel en modo edición; se mantiene `editPrompt` para el prompt suelto.
- **Auditoría F3**: round-trip real crear→editar (cambiar horas y prompt)→leer y comprobar.

### F4 — Compactar/resumir la conversación (a confirmar por investigación)
- Opción por tarea para conversaciones largas en modo continuar/derivar. **El disparador NO es «exit 1» genérico** (taparía fallos reales): se detecta la condición concreta de contexto excedido. El mecanismo exacto (autocompactación nativa de Claude Code vs. resumen-y-reinicio) se fija con la investigación en curso; si Claude Code ya autocompacta en headless, F4 puede ser solo exponer/confirmar ese comportamiento y no reintentar a ciegas.
- **Auditoría F4**: según el mecanismo confirmado.

### Auditoría final
- Toda la suite Go + cross-compila; unit+integración de la extensión; E2E real (crear por panel, multi-hora, editar); empaquetar VSIX; publicar 0.2.0 por tag.

### Estado tras ejecutar §24 (v0.2.0)

F1-F4 hechas y auditadas. Revisión adversarial (2 pasadas): 7 hallazgos, todos en la ruta de edición del panel, corregidos y verificados por E2E (editar limpia presupuesto/autocompact y conserva timeout; presupuesto valida coma decimal; repo bloqueado al editar; panel único cierra-y-recrea). Go verde en 3 plataformas, extensión 19+4, cross-compila. Publicado como 0.2.0.

## 25. Plan auditado — Vista de resultado: Webview (B) + resumen (C) (v0.3.0)

Fecha: 2026-08-19. Problema: `RunDetailsProvider` (src/ui/diff.ts) genera Markdown cuya sección «Timeline» vuelca el `payload` crudo de cada evento (thinking_tokens, firmas base64, campos de cuota) → ilegible. Objetivo: transcripción legible tipo chat con cabecera de resumen.

### Arquitectura (clave para auditar: la lógica es pura y testeable)
- **`src/core/transcript.ts` (PURO, sin `vscode`)**: `buildTranscript(run: Run, events: RunEvent[]): TranscriptModel`.
  - Parsea cada `payload` (JSON) por tipo y produce un modelo de items:
    - `sesion_iniciada` → item `system` («iniciada en worktree X» o «en el repo real»).
    - `assistant` → por bloque de `content`: `text`→item `say`; `tool_use`→item `tool` (nombre + comando/entrada, resumido); `thinking` con texto→item `thought` plegable (los vacíos se descartan).
    - `user`/`tool_result` → se **empareja** con el `tool_use` anterior por `tool_use_id`; su salida va dentro del item `tool` (truncada, plegable).
    - `result` → item `final` (respuesta + `total_cost_usd`, `num_turns`, `is_error`, subtype).
    - Ruido descartado: `thinking_tokens`; `rate_limit_event` se reduce a una nota solo si `status!="allowed"`.
  - No hace red ni E/S; la redacción de secretos ya la hizo el worker. Devuelve también el resumen (estado, coste, vueltas, duración, nº ficheros, sesión, decisión).
- **`src/ui/runView.ts` (Webview)**: renderiza el `TranscriptModel`. CSP+nonce, tema por variables `--vscode-*` (como taskPanel). Cabecera de resumen (C) arriba: nombre, pastilla de estado, coste/vueltas/duración/ficheros; botones Aceptar/Rechazar/Archivar/Ver diff cuando aplican (reusan los comandos existentes). Cuerpo: la transcripción (mensajes como prosa, herramientas plegables, respuesta final destacada).
- **Refresco en vivo**: al abrir, `ctl.events(runId)`; se re-pide y re-renderiza cuando el worker toca el marcador (la extensión ya observa `cambios.marca`), para runs en marcha.
- **Cableado**: `taskkeeper.showRun` abre el Webview en vez de `markdown.showPreview`. El `RunDetailsProvider`/Markdown se conserva como export por si se quiere, pero deja de ser la vista por defecto.

### Auditoría
- **Unitarias de `buildTranscript`** con fixtures de eventos REALES (la ejecución `prueba2`: tailscale/ping): (1) empareja tool_use↔tool_result; (2) extrae el texto del asistente y la respuesta final; (3) descarta thinking_tokens y firmas; (4) run fallida → item final con error; (5) run con ficheros cambiados → resumen con nº. Sin `vscode`, corren en el runner de mocha.
- **Generador de HTML**: nonce presente, CSP presente, sin `http` externo (como el test del panel si se añade).
- **Integración**: `showRun` abre el Webview sin lanzar excepción.
- **Real**: ejecutar una tarea, abrir el resultado, comprobar que se ven mensajes/herramientas/respuesta y el coste.
- **l10n**: cadenas nuevas en ES+EN, `l10n-sync` a cero.

### Riesgos / decisiones
- Un `payload` no-JSON (línea suelta del agente) → item `log` de reserva, nunca rompe el parseo.
- Coste de re-render en runs largos: se limita a los últimos N items visibles con «ver todo».
- B no borra A: A (Markdown) queda disponible; B pasa a ser la vista principal.

## 26. Plan auditado — Ejecutar «en la conversación», en el repo real (v0.3.0)

Fecha: 2026-08-19. Hoy el runner SIEMPRE crea worktree (runner.go:203) y el resultado espera revisión. Objetivo: un segundo eje —**dónde trabaja**— con dos valores: `aislada` (actual) y `en_conversacion` (sin worktree, en el repo real, sin paso de revisión).

### Modelo
- Dos ejes independientes: **conversación** (new/resume/fork, ya existe) y **workspace** (`isolated` | `direct`). «Como si escribiera en la conversación» = `resume` + `direct`.
- Store: columna `workspace_mode TEXT NOT NULL DEFAULT 'isolated'` vía `migrar()`. Campo en `store.Task`, en `aJSON`, en el modelo TS, en `CreateParams`/`createArgs` (`--workspace`), y opción en el panel.

### Runner (packages/runner/runner.go)
- Si `workspace_mode == 'direct'`:
  - **No** llama a `gitwt.Crear`. `cmd.Dir` y `Peticion.DirTrabajo` = `proj.WorkspacePath` (el repo real). No se pasan `--worktree`/`--add-dir` de worktree.
  - **Salta** verifying/diff/review: al terminar el agente, `running → completed` (estado terminal nuevo) o `failed`. No hay worktree que fundir ni Aceptar/Rechazar.
  - `resume`/`fork` siguen funcionando: la conversación real se continúa (ya lo hace el adaptador), así que sus turnos quedan **dentro** de la conversación señalada.
- Estado nuevo `StateCompleted` en la máquina de estados (`allowed`: `StateRunning → StateCompleted`); la bandeja/vistas lo tratan como terminal «hecho, ya está en tu repo». `GetRunDetalle`/inbox lo incluyen sin botón de aceptar.

### Seguridad (lo esencial)
- `direct` + `auditoria` (solo lectura): **inofensivo** (no escribe ficheros). Se permite sin fricción.
- `direct` + `cambios_aislados`: contradicción → se convierte en **«cambios directos»**: escribe en tu checkout real (acceptEdits) SIN posibilidad de rechazar. `git push/merge/gh` siguen prohibidos por el perfil. Por eso:
  - En el panel, **desactivado por defecto**, en rojo, con aviso al guardar y una confirmación modal explícita antes de crear/editar.
  - Aviso si el checkout tiene cambios sin guardar (se mezclarían con los del agente).
- La ranura de concurrencia (una a la vez) se sigue tomando.

### Panel (src/ui/taskPanel.ts)
- Interruptor **«Dónde trabaja: Aislada / En la conversación»** justo bajo «Conversación». Por defecto Aislada.
- Si `en_conversacion` + `cambios_aislados` → etiqueta pasa a «cambios directos» en rojo + confirmación al enviar.
- Resumen lateral refleja el modo.

### Auditoría
- **Store**: test de que `migrar()` añade `workspace_mode` en bases nuevas y viejas; round-trip crear/leer.
- **Runner** (`//go:build windows || darwin`, agente falso): (1) `direct` NO crea worktree y `cmd.Dir` = repo real; (2) termina en `completed`, sin worktree en disco; (3) `isolated` sigue creando worktree y acabando en `awaiting_review` (no-regresión).
- **ctl**: `--workspace` round-trip; `previsualizar` no se ve afectado.
- **Estados**: test de que `StateRunning → StateCompleted` es válida y `completed` es terminal.
- **Real E2E**: crear tarea `direct` + `resume` + solo lectura, ejecutar, comprobar que NO hay carpeta de worktree, que la conversación real recibió los turnos, y que la run acabó `completed` sin diff.
- **Seguridad**: test/manual de que el panel exige confirmación en «cambios directos».

### Riesgos / decisiones
- `direct` quita la red de revisión: es una elección explícita del usuario, marcada y confirmada. Por defecto todo sigue aislado.
- No se toca el flujo aislado: es una rama nueva en el runner, no un cambio del camino existente (menos riesgo de regresión).
- `completed` evita reutilizar `accepted` (que implica «fundido desde un worktree», aquí no aplica).

### Estado tras §25 + §26 (v0.3.0)

Ejecutadas y auditadas. §25: transcript.ts puro (6 tests con eventos reales) + runView.ts (Webview, resumen + transcripción + acciones + refresco en vivo); showRun abre el Webview, el crudo queda en showRunRaw. §26: estado `completed` + workspace_mode (direct/isolated) por columna migrada; runner con rama directa sin worktree; ctl --workspace validado; panel con interruptor «Dónde trabaja» + confirmación de «cambios directos». Auditoría adversarial en 3 pasadas (5+3+1 accionables), todo corregido y verificado, incluida la regla de no-emojis (SVG inline). Go 3 plataformas, extensión 25 unitarias + 4 integración, E2E real de directo (sin worktree, completed). Publicado 0.3.0.

### Ajuste v0.3.1 — modo directo sin Git

«En la conversación» (direct) ya no exige repositorio Git: `gitwt.ComprobarSuave` (solo carpeta existe; helper `raizGit` compartido con `Comprobar`), usado por `ctl crear` y el runner en modo directo; `editar` a aislado sobre carpeta sin git se rechaza al editar; el panel oculta el aviso «no es git» en directo. Auditado (4 hallazgos: docs, dedup, validación editar, swallow intencional). Tests: `TestComprobarSuave`, `TestDirectoSinGitFunciona`.

> Nota de estado: §24-§26 publicadas (0.2.0-0.3.x). Después: **0.4.0** autodetección de carpeta desde el id de sesión (`sessions.ts::foldersForSession`); **0.5.0** creación por intención (dos atajos = presets sobre los ejes modo×workspace, `derivePreset`) + tutorial interactivo (Walkthrough nativo ES/EN, `contributes.walkthroughs`, SVG propio). El binario `editar` IGNORA `--proyecto` (verificado: `main.go` parte de `nueva := *base`).

## 27. Plan auditado — Cinco mejoras (v0.6.0 → v0.8.0)

Fecha 2026-08-20. Cinco mejoras priorizadas por encaje con la promesa («los agentes trabajan de noche; revisas por la mañana; nada toca tu repo hasta que aceptas; local-first»). Secuencia final (tras auditoría, v0.6.0 se dividió): **v0.6.0** = §27.1; **v0.6.1** = §27.2; **v0.7.0** = §27.3; **v0.8.0** = §27.4; pista paralela §27.5 (macOS/firma). Cada fase se audita antes de publicar por tag. Ver «Correcciones tras auditoría» al final.

Principios que respetan las cinco: sin demonio (la extensión lo lee todo por `ctl --json`; la lógica pura vive en Go); nunca emojis (SVG propio/codicons); paridad ES/EN (`bundle.l10n.es.json` + `package.nls*.json`, `l10n-sync` 0/0); columnas nuevas SOLO en `store.migrar()`; comandos nuevos con el patrón `salida.ok/fallo` + struct con tags json. Anclas verificadas: `store.go`/`extra.go` (acceso a datos), `ctl/main.go` (dispatch + `ejecucionJSON`/`tareaJSON`/`opcionesTarea`/`registrar`), `runner.go` (estados terminales), `platform` (build-tags win/darwin, `LanzarWorker`/`RegistrarReintento`/`Exists`), `ctl.ts` (clase `Ctl`, `CreateParams`, `createArgs`), `trees.ts`/`model.ts`/`taskPanel.ts`.

### 27.1 — Resumen matinal + salud del planificador (v0.6.0)

**Objetivo.** El valor es «despiertas y revisas lo de la noche», pero sin demonio el fallo más grave es SILENCIOSO (máquina suspendida a las 03:00 → no disparó; o disparó y falló auth). Dos entregables: (a) **resumen de anoche** al abrir VS Code; (b) **vista de salud** que detecta disparos perdidos y —lo más valioso— tareas cuyo disparador del SO se ha desregistrado solo.

**Store** (`packages/store/extra.go`):
```go
type ResumenNoche struct {
    DesdeUTC        string  `json:"desde_utc"`
    Terminadas      int     `json:"terminadas"`       // completed + accepted
    EsperanRevision int     `json:"esperan_revision"` // awaiting_review
    Fallidas        int     `json:"fallidas"`         // failed*
    Saltadas        int     `json:"saltadas"`         // skipped (incluye misfire)
    EnCurso         int     `json:"en_curso"`         // running/queued/preflight/verifying
    CosteTotalUSD   float64 `json:"coste_total_usd"`
}
// Agrega las runs terminadas desde `desdeUTC` (RFC3339) más las que siguen en curso.
func (db *DB) ResumenNoche(desdeUTC string) (ResumenNoche, error) {
    r := ResumenNoche{DesdeUTC: desdeUTC}
    rows, err := db.Query(`SELECT status, cost_usd FROM runs
        WHERE finished_at >= ?1 OR status IN ('running','queued','preflight','verifying')`, desdeUTC)
    if err != nil { return r, err }
    defer rows.Close()
    for rows.Next() {
        var st string; var cost sql.NullFloat64
        if err := rows.Scan(&st, &cost); err != nil { return r, err }
        switch State(st) {
        case StateCompleted, StateAccepted, StateRejected: r.Terminadas++ // resueltas (hecha/aceptada/descartada): reconcilia el coste
        case StateAwaitingReview:           r.EsperanRevision++
        case StateSkipped:                  r.Saltadas++
        case StateRunning, StateQueued, StatePreflight, StateVerifying: r.EnCurso++
        default: if strings.HasPrefix(st, "failed") { r.Fallidas++ } // cancelled cae aquí sin sumar (su coste ~0)
        }
        if cost.Valid { r.CosteTotalUSD += cost.Float64 }
    }
    return r, rows.Err()
}
// NOTA: extra.go hoy importa solo database/sql y errors → añadir "strings".
// Por tarea Enabled: disparos perdidos (skipped POR MISFIRE) y pendientes manual en la ventana.
// OJO: `skipped` lo generan tres caminos: misfire (runner.go:101), sin-turno-libre
// (runner.go:157) y tope de gasto (§27.2). Contar por ESTADO daría falsos rojos:
// se cuenta por MOTIVO leyendo la auditoría (action 'state_skipped' cuyo detalle
// habla de «retraso», y 'pendiente_de_decision'). Además EXCLUYE tareas con
// depends_on_task_id!='' (§27.4: sin disparador por diseño).
type SaludBaseTarea struct { TareaID, Tarea string; DisparosPerdidos, Pendientes int }
func (db *DB) SaludBase(desdeUTC string) ([]SaludBaseTarea, error) {
    // SELECT t.id,t.name ... FROM tasks t WHERE t.enabled=1 AND COALESCE(t.depends_on_task_id,'')=''
    // subconsultas a `audit` por run de la tarea: misfire (detalle LIKE '%retraso%') y 'pendiente_de_decision'.
}
```

**Plataforma** (`packages/platform`): añadir `TareaExiste(taskID string) bool` por build-tag, delegando en lo que YA existe: `dwin.Exists` (darwin, `launchd.go:76`, `os.Stat` del plist) y `win.Exists` (`schtask.go:94`, `schtasks /Query /TN … .Run()==nil`, **ya testeado** `schtask_test.go:25,51`). OJO: Ambos **re-envuelven** con `NombreTarea(taskID)` (schtask.go:98, launchd.go:85), así que hay que pasarles el **id crudo** de la tarea, NO `os_trigger_id` (que se guarda ya como nombre completo, `main.go:317`): pasar el nombre completo lo doble-envuelve → siempre `false` → todo en rojo. Es la comprobación honesta: un disparador que se esfumó se pinta en rojo.

**ctl** (`main.go`): `case "resumen"` → `resumen(out, db, cfg, args)`, flag `--horas` (int, default 16). Combina store + `platform`/`scheduler` (que el store no puede tocar):
```go
res, _ := db.ResumenNoche(desdeUTC)
base, _ := db.SaludBase(hace7dUTC) // SaludBase EXCLUYE tareas con depends_on_task_id!='' (§27.4: sin disparador por diseño)
type saludJSON struct{ TareaID, Tarea, ProximaLocal string `json:"..."`; Registrada bool `json:"registrada"`; DisparosPerdidos, Pendientes int `json:"..."` }
var salud []saludJSON
for _, b := range base {
    reg := platform.TareaExiste(b.TareaID) // id CRUDO, no os_trigger_id (ver arriba)
    prox := ""
    if t,_ := db.GetTask(b.TareaID); t.Enabled && reg { // sin disparador registrado, la «próxima» sería una hora falsa: se omite
        if o,_ := scheduler.Next(regla(t), t.Timezone, now); o != nil { prox = o.ResolvedLocal }
    }
    salud = append(salud, saludJSON{b.TareaID, b.Tarea, prox, reg, b.DisparosPerdidos, b.Pendientes})
}
out.ok(map[string]any{"resumen": res, "salud": salud}, func(){ /* texto */ })
```
Añadir línea a `uso()`.

**Extensión**: `Ctl.digest(horas=16): Promise<{resumen,salud}>` (`this.call('resumen','--horas',String(horas))`). `src/ui/digestView.ts`: Webview (patrón `taskPanel.ts`, CSP+nonce) con «Anoche» (stat-tiles SVG + coste) y «Salud» (una fila por tarea; roja si `!registrada` o `disparos_perdidos>0`). Comando `taskkeeper.showDigest` + botón en `view/title` de la bandeja. **Auto-apertura una vez al día** si hay actividad: en `activate`, **dentro de `if (ctl)`** (en plataformas sin binarios `ctl` es undefined) y tras `refreshAll`, si `resumen.Terminadas+Fallidas+EsperanRevision>0` y `globalState 'digestShownDate' != hoy(local)` → abrir con `createWebviewPanel(..., { viewColumn: vscode.ViewColumn.Beside, preserveFocus: true })` y guardar la fecha. OJO: `ViewColumn.Beside` a secas (como en runView.ts:39) **sí toma el foco**; hace falta el objeto con `preserveFocus:true` para no robarlo al arrancar. El precedente del walkthrough (extension.ts:314-321) respalda el *mecanismo* (globalState + comando en activate) pero abre CON foco a propósito — aquí se diverge conscientemente.

**Riesgos / auditoría**: «anoche» = ventana móvil `--horas 16` (documentar, no «desde las 00:00»); `TareaExiste` en Windows puede tardar (timeout/cachear; darwin es `os.Stat`, barato); auto-apertura acotada a una/día con actividad. Tests: store (round-trip de contadores con runs sembradas + límite temporal), `TareaExiste` (win/darwin, inexistente→false), ctl (`resumen` emite el JSON), y unidad del render si se extrae puro.

### 27.2 — Panel de gasto + tope mensual (v0.6.0)

**Objetivo.** Convertir el coste (fuente de ansiedad) en algo visible y **controlable**: gasto por tarea/día/mes + un **tope mensual de máquina** que el worker respeta.

**Store** (`extra.go`):
```go
type GastoTarea struct { TareaID, Tarea string `json:"..."`; Runs int `json:"runs"`; USD float64 `json:"usd"` }
type GastoDia   struct { Dia string `json:"dia"`; USD float64 `json:"usd"` }
func (db *DB) GastoMes(desdeMesUTC string) (float64, error)               // SUM(cost_usd) WHERE finished_at>=?
func (db *DB) GastoPorTarea(desdeUTC string) ([]GastoTarea, error)        // GROUP BY task_id JOIN tasks
func (db *DB) GastoPorDia(desdeUTC string) ([]GastoDia, error)            // GROUP BY substr(finished_at,1,10)
```
Tope mensual como ajuste de máquina (patrón `ClaveCupo`): `const ClaveTopeMes="monthly_budget_usd"`, `db.TopeMes() float64`, `db.FijarTopeMes(v float64)`.

**Runner** (enforcement). OJO: Colocarlo **antes** de tomar el turno (`turns.Acquire`, runner.go:155) y de crear el worktree (runner.go:215-223): si se salta después, deja un **worktree huérfano** y consume un turno para nada. Punto correcto: justo tras `CreateRunIfAbsent` (estado `queued`; `queued→skipped` es válido, store.go:405), antes del preflight pesado. La variable de tiempo en `Ejecutar` es `ocurrencia`, no `ahora`:
```go
if tope := db.TopeMes(); tope > 0 {
    if gasto, _ := db.GastoMes(inicioDeMesUTC(ocurrencia)); gasto >= tope {
        db.Transition(run.ID, store.StateSkipped,
            fmt.Sprintf("tope mensual de %.2f USD alcanzado (%.2f gastados)", tope, gasto))
        return nil
    }
}
```
Tope **blando** (dos workers simultáneos podrían pasarse un poco; aceptable — evita el goteo, no un pico). `inicioDeMesUTC`, `db.TopeMes`, `db.GastoMes` son nuevos. **CONFIRMADO: `daily_budget_usd` NO se aplica hoy** (el runner solo pasa `MaxBudgetUSD` por-run, runner.go:263) → cablear también el diario aquí (SUM por `task_id` con `finished_at` de hoy). OJO: **CONFIRMADO: Codex NO reporta coste** (`codex.go` no rellena `CosteUSD`; solo `claude.go` desde `total_cost_usd`, claude.go:164,188) → en una máquina con tareas Codex el tope es **inoperante** (`GastoMes`≈0) y el panel infravalora. La UI debe avisar («coste informado; Codex no lo reporta») y advertir al fijar tope si hay tareas Codex.

**ctl**: `case "gasto"` → `{mes_usd, tope_mes_usd, por_tarea, por_dia}`; `case "tope-mensual"` (get sin args / set con `<usd>`, patrón `cupo`).

**Extensión**: `Ctl.spend()` + `Ctl.setMonthlyCap(usd)`. `src/ui/spendView.ts`: Webview con barra mes-vs-tope (color de aviso al acercarse), barras por día (SVG propio, sin librerías — regla de [dataviz]), tabla por tarea, y control del tope. Comando `taskkeeper.showSpend` + botón en `view/title`.

**Riesgos / auditoría**: el coste solo se registra si el adaptador devuelve `CosteUSD` — **Codex puede no reportarlo** → el total infravalora; mostrarlo como «coste informado» y avisar (auditar `adapters/codex.go`). Mes por UTC (documentar). Race blanda aceptada. Tests: store (agregados + límites de fecha), tope (get/set meta), runner (`TestTopeMensualSalta`: tope 0.01 + gasto previo → run nueva `skipped` con motivo; sin tope corre), ctl round-trip.

### 27.3 — Plantillas de tarea (v0.7.0)

**Objetivo.** Bajar la barrera de la página en blanco (encaja con el tutorial): catálogo pequeño de tareas típicas que precargan nombre/prompt/perfil/workspace/horario. Casi todo frontend (el prompt es texto; perfil/workspace/regla ya son campos).

**Datos** (`src/ui/templates.ts`, textos por `vscode.l10n.t`):
```ts
// dias es obligatorio para 'weekly' (si no, cae al [1]=lunes por defecto, ambiguo).
export interface Plantilla { id:string; nombre:string; desc:string; prompt:string;
  perfil:'auditoria'|'cambios_aislados'; workspace:'isolated'|'direct'; regla:'daily'|'weekly'; dias:number[]; horas:string[]; }
// OJO: Comillas DOBLES en textos con apóstrofo (con comilla simple `week's` rompe
// el literal y NO compila). NUNCA backticks: `l10n-sync` extrae por regex de
// t('…')/t("…") y NO ve los template literals → quedarían sin traducir en silencio.
// Multilínea: t("linea1\nlinea2"). `plantillas()` se llama en el HANDLER (al
// construir `init`), no a nivel de módulo, o `t()` devolvería inglés sin bundle.
export function plantillas(): Plantilla[] { return [
  { id:'deps',     nombre:t('Update dependencies'), desc:t('…'), prompt:t("Check for outdated dependencies and open the updates in a worktree for review."), perfil:'cambios_aislados', workspace:'isolated', regla:'weekly', dias:[1], horas:['03:00'] },
  { id:'changelog',nombre:t('Draft changelog'),     desc:t('…'), prompt:t("Summarise this week's merged changes into a changelog entry. Read only."),        perfil:'auditoria',        workspace:'isolated', regla:'weekly', dias:[5], horas:['08:00'] },
  { id:'lint',     nombre:t('Lint sweep'),          desc:t('…'), prompt:t("Run the linter and fix what is safe, in a worktree for review."),                  perfil:'cambios_aislados', workspace:'isolated', regla:'daily',  dias:[],  horas:['03:00'] },
  { id:'fixtests', nombre:t('Fix failing tests'),   desc:t('…'), prompt:t("Run the test suite; if anything fails, fix it in a worktree for review."),         perfil:'cambios_aislados', workspace:'isolated', regla:'daily',  dias:[],  horas:['03:00'] },
  { id:'blank',    nombre:t('Blank'),               desc:t('Start from scratch.'), prompt:'', perfil:'cambios_aislados', workspace:'isolated', regla:'daily', dias:[], horas:['03:00'] },
]; }
```
(4 plantillas + «en blanco»; las que cambian código, en `isolated` → siempre revisables; ninguna en «cambios directos».)

**Panel** (`taskPanel.ts`): el host ya envía `promptTemplate` en `init` → se amplía a `plantillas` (resueltas con `t()` en el host). En `render()`, encima de «¿Qué tipo de tarea?», una fila de tarjetas «Plantilla»; al elegir, precarga `state.nombre/prompt/perfil/workspace/regla/horas` y recalcula `derivePreset(...)`. El usuario ajusta libremente. **Sin cambios de backend.**

**Riesgos / auditoría**: prompts seguros y buenos (los que cambian código, isolated por defecto); localizar prompts multilínea por `bundle.l10n.es.json` (verifica `l10n-sync`); mantenerlo pequeño (5). Tests: unidad de que `plantillas()` devuelve valores en dominio (perfil/workspace válidos) y de que aplicar una plantilla fija el estado esperado (extraer a función pura); no-regresión (plantilla «en blanco» = hoy).

### 27.4 — Encadenar tareas / acción ante fallo (v0.8.0) — la más compleja

**Objetivo.** «Ejecuta B cuando A termine bien», «si A falla, reintenta o avísame».

**Alcance v1 (acotado a un punto de despacho único):** el **padre debe ser modo directo** (`workspace_mode='direct'`), cuyo estado terminal (`completed`/`failed*`) lo fija el **worker** en un solo sitio. El encadenado desde padres **aislados** (donde «éxito» = el usuario *acepta*, y habría que despachar también en `ctl aceptar`) se deja como continuación. Evita dos puntos de despacho y semántica ambigua.

**Store** (`migrar()` + métodos). Columnas nuevas SOLO en `migrar()`:
```go
"depends_on_task_id": "TEXT NOT NULL DEFAULT ''",
"trigger_on":         "TEXT NOT NULL DEFAULT ''",   // success | failure | always
```
Campos en `store.Task`, en `GetTask`/`CreateTask`/`editar`. Método:
```go
// Tareas activas cuyo padre es taskID y cuyo disparo casa con el resultado; 'always' casa con todo.
func (db *DB) DependientesDe(taskID, resultado string) ([]*Task, error) {
    // SELECT id ... WHERE enabled=1 AND depends_on_task_id=? AND (trigger_on=? OR trigger_on='always')
}
```

**Runner** (despacho). OJO: NO vale una llamada única al final de `Ejecutar`: hay ~15 `return nil` tempranos, y el éxito de **modo directo** —el caso objetivo— transiciona a `completed` y **retorna en runner.go:314-315**, no en :348 → se perdería todos los `completed` directos. Solución: un `defer` registrado **justo tras confirmar `creada`** (después de runner.go:152, capturando `run.ID`), que lea el estado FINAL con `db.GetRun(run.ID)` y despache:
```go
run, creada, err := db.CreateRunIfAbsent(...)
if err != nil || !creada { return ... } // no despachar en el no-op de idempotencia ni con run==nil
defer func() {
    if r, e := db.GetRun(run.ID); e == nil { dispararDependientes(db, o, task.ID, r.Status) }
}()
// ...
func dispararDependientes(db *store.DB, o Opciones, padreID string, estado store.State) {
    var res string
    switch {
    case estado == store.StateCompleted: res = "success"
    case estado == store.StateFailedQuota: return          // tiene reintento programado: no dispares fallo aún
    case esFallo(estado):                res = "failure"    // failed / failed_verification / failed_auth
    default: return                                         // skipped/cancelled/awaiting_review: v1 no encadena (padres aislados fuera)
    }
    deps, err := db.DependientesDe(padreID, res); if err != nil { return }
    for _, dep := range deps {
        if o.LanzarDependiente != nil { _ = o.LanzarDependiente(dep.ID) } // worker: platform.LanzarWorker(cfg.Worker, dep.ID, time.Now().UTC())
    }
}
```
`o.LanzarDependiente` (nuevo campo de `Opciones`) lo inyecta el worker como ya hace con `RegistrarReintento` (worker `main.go:83-85`). El dependiente arranca como ocurrencia manual nueva → su propia run idempotente. Que `skipped`/`cancelled`/`awaiting_review` caigan en `default→return` deja **correctamente** a los padres aislados fuera de v1.

**Sin disparador propio (una tarea dependiente se dispara SOLO por el padre):** el salto del registro se hace en los **call-sites** de `registrar()` (su firma `(cfg,taskID,zona,r,occ)` no ve `depends_on`): `crear` (main.go:313), `editar` (main.go:393) **y `activar`/reanudar** (main.go:646) — si no, una dependiente pausada y reanudada volvería a registrar disparador. Y `crear` hace `SetOSTriggerID` incondicional (main.go:317): hacerlo **condicional** (no fijar id de disparador si es dependiente), o quedaría un `os_trigger_id` obsoleto. **Acople con §27.1:** las dependientes están `Enabled` pero sin disparador **por diseño** → `SaludBase` DEBE excluirlas (ya reflejado arriba), o toda dependiente saldría como falso rojo «desregistrado».

**Prevención de ciclos (crítico):** con un solo padre (`depends_on_task_id`) el grafo es un bosque; solo `editar` puede cerrar un ciclo (al `crear`, la tarea nueva no tiene hijos). `ctl crear/editar` valida: el padre existe y, subiendo del padre propuesto a la raíz, no reaparece el id que se edita; profundidad máxima (p. ej. 10) como red secundaria.

**ctl**: flags `--depende-de <taskID>` y `--disparar-en <success|failure|always>` en `parsearOpciones` (validación existencia+ciclos); `registrar` salta el disparador si hay dependencia; `aJSON` expone `depends_on_task_id`/`trigger_on`.

**Extensión**: `CreateParams`+`createArgs`: `dependeDe?`/`dispararEn?` → `--depende-de`/`--disparar-en`. Panel (avanzado): selector «Se ejecuta después de… [tarea] · en [éxito/fallo/siempre]». OJO: **Contrato «sin horario»:** hoy `submit()` valida `horas`/`okTime` **incondicionalmente** (taskPanel.ts:740-745) y `toCreateParams` construye `hora` de `horas`; al fijar dependencia hay que **ramificar la validación** (saltar el check de hora) y decidir el contrato con `ctl crear` (aceptar `--regla`/`--hora` de una regla que `registrar` simplemente no registra, para no romper el modelo de la tarea). Sin esto, una dependiente **no se puede enviar**. **Vista de tareas: badge, NO anidar.** `TasksProvider.getChildren` es plano y `TaskItem.id = task:${id}` (trees.ts:13,37): anidar la misma tarea en raíz y bajo el padre **colisiona el id** (comportamiento impredecible) y **duplica** salvo filtrar la raíz. Para v1, un **badge** en `TaskItem.description` («tras "Padre" · éxito», con codicon `$(arrow-small-right)` no un carácter suelto si va como icono); el árbol jerárquico queda como continuación.

**Riesgos / auditoría (mayor superficie):** ciclos → validación + test A→B→A rechazado; tormenta de fallos → acotación a padre directo + profundidad máxima; idempotencia → cada disparo del dependiente es ocurrencia manual «ahora», run nueva (correcto); padres aislados fuera de v1 (dejarlo claro en UI). Tests: store (migración; `DependientesDe` success/failure/always), ctl (ciclo rechazado; `registrar` salta disparador con dependencia), runner (`TestDispararDependientes` con `LanzarDependiente` falso: `completed`→success+always, `failed`→failure+always).

### 27.5 — macOS + quitar preview + firma (pista paralela)

**Estado.** macOS **ya está implementado**: `platform/tareas_darwin.go` + `platform/darwin/{launchd.go,group.go}` (launchd, plist, grupo de procesos), y `build-bin.mjs` cross-compila `darwin-x64`/`darwin-arm64`; el runtime ya es multiplataforma (`binaries.ts`). Falta empaquetado/publicación, verificación en hardware y firma.

**Técnico (agente puede):**
- **release.yml — reestructura en 3 jobs** (hoy es UN job monolítico con verify-tag/go-test/npm-check + build+publish de solo `win32-x64`). No es un añadido, es un refactor:
  1. `verificar` (una vez): verify-tag, `go test ./...`, `npm run check`.
  2. `publicar` (matriz `{win32-x64, darwin-x64, darwin-arm64}`, `needs: verificar`, `fail-fast: false`): `build-bin.mjs <target>` + `vsce publish --target <target>` + `ovsx publish --target <target>`.
  3. `release` (una vez, `needs: publicar`): `gh release create` con los 3 VSIX.
- OJO: **BLOQUEANTE — la idempotencia actual es por VERSIÓN, no por (versión,target).** `release.yml:67` salta si `versions.some(v.version===$VER)` y ovsx igual (`:82`). En una matriz de misma versión, en cuanto `win32-x64` publique vX, las patas darwin ven vX presente → **SKIP → darwin nunca se publica**. Hay que rehacer la comprobación por `(version, targetPlatform)` (vsce/ovsx exponen el target del paquete) o quitar el skip y confiar solo en el «already exists/published».
- OJO: **CI solo compila darwin, no lo ejecuta.** CI corre en `ubuntu-latest`; `go test ./...` ahí compila el **stub** `!windows&&!darwin` (`platform_other.go`), NO `launchd.go`/`tareas_darwin.go`. `GOOS=darwin go build ./...` (lo hace build-bin) es **compile-check**, no test. Decir «compile-check», no «test multiplataforma»; el runtime darwin queda a verificación humana en Mac.
- **Textos «preview» a quitar en GA** (más de los citados): `package.json:31` (`"preview": true`), `README.md:40` («Windows 10/11, x64 in this release…»), y `release.yml:98` (nota de release **hardcodeada** «Windows x64 preview»).
- **Orden respecto a §27.1:** darwin GA debe ir **después** de que aterrice la rama darwin de `platform.TareaExiste` (§27.1), o los usuarios de Mac verían la vista de salud rota.

**Humano (bloqueado):** verificación en Mac real (`launchctl`, despertar de suspensión, permisos LaunchAgent); **firma** — Authenticode (Windows, mata SmartScreen del worker) + Developer ID + notarización (Apple, para los binarios darwin). Requieren certificados del titular; sin ellos Gatekeeper bloquea el binario no firmado.

**Riesgos / auditoría**: no publicar darwin como GA hasta verificar en Mac; el VSIX por plataforma no debe romper win32 (jobs separados, versión común); README debe marcar macOS como preview hasta firmar.

### Secuencia y auditoría global
Tras auditoría, se **divide v0.6.0**: §27.1+§27.2 juntos serían dos webviews nuevos + cambio de runner + primitiva OS nueva en un tag (demasiada superficie para «2-3 pasadas»). Secuencia final: **v0.6.0** = §27.1 · **v0.6.1** = §27.2 · **v0.7.0** = §27.3 · **v0.8.0** = §27.4 · pista paralela §27.5 (GA de macOS tras la rama darwin de §27.1 + Mac + certificados). Cada fase: afinar aquí → ejecutar → **auditar adversarial (2-3 pasadas)** → arreglar → `npm run check` verde + `go test ./...` → publicar por tag.

OJO: **Paridad de manifiesto no automática:** `l10n-sync` solo valida `bundle.l10n.es.json` (strings `t()`), NO los `.nls`. Los comandos nuevos (`taskkeeper.showDigest`, `showSpend`) exigen `%cmd.showDigest%`/`%cmd.showSpend%` en `package.nls.json` Y `package.nls.es.json` a mano.

### Correcciones tras auditoría (2 revisores adversariales)
Backend (viabilidad Go) + Frontend/UX/alcance, contrastados contra el código real. Bloqueantes/altos ya corregidos arriba, en línea:
- **§27.1 · `TareaExiste(id crudo)`**, no `os_trigger_id` (doble-envoltura → todo rojo). `win.Exists` YA existe y está testeado. `SaludBase` cuenta `skipped` por MOTIVO (no por estado; hay 3 orígenes) y excluye dependientes. `ResumenNoche` cuenta `rejected` (reconcilia el coste) y necesita `import "strings"`. Auto-abrir con `preserveFocus:true` dentro de `if(ctl)`. «Próxima» se omite si `!registrada`.
- **§27.2 · Enforcement del tope ANTES del worktree/turno** (si no, worktree huérfano); var `ocurrencia`. Codex NO reporta coste → tope inoperante con Codex + panel infravalora (avisar en UI). `daily_budget_usd` confirmado sin aplicar → cablearlo aquí.
- **§27.3 · Comillas dobles** en textos con apóstrofo (con simple NO compila); **nunca backticks** (invisibles a `l10n-sync`); `dias` obligatorio en `weekly`; `plantillas()` en el handler.
- **§27.4 · Despacho por `defer`** tras `creada` (una llamada al final pierde los `completed` directos que retornan en :315). Excluir `failed_quota` (tiene reintento). Salto de `registrar` también en `activar`; `SetOSTriggerID` condicional. `SaludBase` excluye dependientes. UI: **badge, no anidar** (colisión de `id`); ramificar la validación de `submit()` para «sin horario».
- **§27.5 · Idempotencia por (versión,target)** o darwin no se publica nunca; reestructurar en 3 jobs; CI solo compila darwin (compile-check); quitar «preview» también de `README.md:40` y `release.yml:98`.
- **Alcance:** dividir v0.6.0/v0.6.1; darwin GA supeditada a §27.1.

### Estado tras ejecutar §27 — las 5 mejoras ENTREGADAS (2026-08-20)
Ejecutadas por fases, cada una con auditoría adversarial (1-2 revisores) y arreglos antes de publicar por tag. `go test ./...` + `npm run check` verdes en cada una.
- **v0.6.0 — §27.1 Resumen matinal + salud.** `store.ResumenNoche`/`SaludBase`, `platform.TareaExiste`, `ctl resumen`, `digestView` (webview, auto-apertura una-vez-al-día sin robar foco). Auditoría: 2 medios + 2 bajos (cancelled sumaba coste sin contador; EnCurso contaba queued huérfanas; motivo de misfire estructurado; pendientes marca fila). Publicado.
- **v0.6.1 — §27.2 Gasto + tope mensual.** `store` agregados + `TopeMes`, `runner.topeSuperado` (mensual de máquina + diario por tarea, antes sin aplicar), `ctl gasto`/`tope-mensual`, `spendView`. Auditoría: 2 medios + 2 bajos (ventana en `now` no en `ocurrencia`; re-chequeo tras el turno para cupo=1; eje 14 días continuo; validación del campo). **Fallo de CI resuelto:** faltaba `//go:build windows` en `tope_test.go` → el panel llegó a las tiendas dentro de v0.7.0. Codex no reporta coste (confirmado, avisado en UI).
- **v0.7.0 — §27.3 Plantillas.** `src/ui/templates.ts` (catálogo localizado), fila en el panel, «Ninguna» restaura el andamio. Auditoría: 0 bugs, 2 nits. Publicado (lleva también v0.6.1).
- **v0.8.0 — §27.4 Encadenar tareas.** Columnas `depends_on_task_id`/`trigger_on`, `DependientesDe`, `runner.dispararDependientes` por `defer`, `worker.LanzarDependiente`, ctl con validación de ciclos (visitados) y salto de disparador, panel con selector + badge en el árbol. Auditoría: 1 alto (borrar padre con dependientes → BLOQUEADO) + 2 medios (auditoría del despacho; test de ctl para ciclos) + bajos, corregidos. Publicado.
- **§27.5 — macOS/firma (parcial).** Hecho por el agente: darwin cross-compila con todos los cambios (verificado arm64+amd64) y se añadió un **compile-check de darwin en cada release** (`release.yml`) para que no se rompa en silencio. **Bloqueado por el humano (no lo puede hacer el agente):** (1) certificados de firma — Authenticode (Windows) + Developer ID + notarización (Apple); (2) verificación en un Mac real (`launchctl`, despertar de suspensión); (3) beta corta antes de quitar `"preview"`. Cuando estén: reestructurar `release.yml` en 3 jobs con idempotencia por (versión,target) y publicar `darwin-x64`/`darwin-arm64`, quitar «preview» de `package.json`/`README`/`release.yml`.
