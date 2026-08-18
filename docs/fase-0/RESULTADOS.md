# Fase 0 — Resultados

Agent Calendar · Argalla · 18 de agosto de 2026
Máquina de pruebas: Windows 11 Pro 26200, dominio `ARGALLA`, usuario `kirne`.

Puertas de la Fase 0 según el plan de ejecución. Una puerta cerrada significa que el diseño se sostiene; una puerta abierta significa que el diseño cambia, no que se apriete el calendario.

| Puerta | Estado | Consecuencia |
|---|---|---|
| P-01 Job Objects | ✅ **Verde** | D4 confirmada por medición. Go queda justificado |
| P-05 Ocurrencias y horario de verano | ✅ **Verde** | 6 pruebas, criterios 1 y 2 incluidos |
| P-02 Tarea de reactivación | 🟡 **Verde parcial** | Registro y `WakeToRun` verificados; falta la suspensión real |
| Codex: protocolo y fork | ✅ **Verde** | `thread/fork` confirmado a nivel de esquema, no solo de documentación |
| P-00 Detección de agentes | ⚠️ **Verde con hallazgo grave** | FR-001 hay que reescribirla |
| P-03 Listado de sesiones de Claude | ✅ **Verde** | 95 sesiones reales; el SDK normaliza rutas |
| P-06 Cuota y credencial | ⚠️ **Verde con hallazgo grave** | El fallo de credencial se reintenta 10 veces: hay que cortarlo |
| P-04 Aislamiento del canal | ✅ **Verde** | Sin puerto TCP; segundo servidor rechazado |
| Condiciones de uso | ✅ **Verde** | Informe en `CONDICIONES-USO.md`, con una lectura humana pendiente |

**Fase 0 cerrada.** Nueve puertas resueltas: seis en verde, dos en verde con hallazgo que corrige el diseño, una en verde parcial cuya medición restante solo tiene sentido en otro hardware.

---

## P-00 · Detección de agentes — hallazgo grave

**Ni `claude` ni `codex` están en el PATH del sistema.** Los dos viven dentro de la carpeta de su extensión de VS Code, con el número de versión en la ruta:

```
~/.vscode/extensions/anthropic.claude-code-2.1.234-win32-x64/resources/native-binary/claude.exe
~/.vscode/extensions/openai.chatgpt-26.814.41407-win32-x64/bin/windows-x86_64/codex.exe
```

Versiones detectadas: `2.1.234 (Claude Code)` y `codex-cli 0.148.0-alpha.15`.

Consecuencias para FR-001, que decía simplemente «detectar Codex y Claude, versión, ruta y estado de autenticación»:

1. **La detección no puede depender del PATH.** Hay que localizar por patrón dentro de `~/.vscode/extensions` y ordenar por versión.
2. **Conviven varias versiones instaladas.** En esta máquina hay `2.1.233` y `2.1.234` de Claude, y dos de Codex. Hay que elegir la más alta con orden de versión, no alfabético: `sort -V`, no `sort`.
3. **La ruta caduca en cada actualización de la extensión.** Guardar la ruta absoluta en la base de datos y no volver a comprobarla es garantizarse un fallo silencioso el día que VS Code actualice. La ruta se resuelve en cada preflight.
4. **Codex es `0.148.0-alpha.15`.** Una versión alfa. La matriz de versiones mínimas de la sección 21 tiene que asumir cambios de superficie entre alfas.

## P-01 · Job Objects — verde

La prueba lanza un intermedio que crea un nieto de larga vida y muere sin esperarlo, dejándolo huérfano. Es el caso exacto que `taskkill /T` no cubre, porque cuando se le pide recorrer el árbol el intermedio ya no existe.

```
nieto 24348 vivo y huérfano (intermedio 31348 muerto)
nieto 24348 muerto tras cerrar el job
--- PASS: TestJobObjectMataNietoHuerfano (0.25s)
--- PASS: TestJobObjectKillAllExplicito (0.01s)
```

`KILL_ON_JOB_CLOSE` cumple: basta cerrar el handle del objeto para que muera toda la descendencia, incluida la huérfana. **Criterio de aceptación 12 cubierto** y decisión D4 confirmada.

No hizo falta la vía de escape de `CreateProcess` con `CREATE_SUSPENDED`. La ventana de carrera entre `Start` y `Adopt` sigue existiendo y sigue documentada, pero no se manifestó.

Código en `packages/platform/windows/job.go`, pruebas en `job_test.go`.

## P-05 · Ocurrencias y horario de verano — verde

```
PASS  TestCriterio1_TareaUnica              25/08/2026 11:00 Europe/Madrid → 2026-08-25T09:00:00Z, una sola vez
PASS  TestCriterio2_SemanalCuatroOcurrencias  cuatro lunes consecutivos correctos
PASS  TestDST_HoraInexistente               29/03/2026 02:30 → 03:30, ajuste registrado
PASS  TestDST_HoraRepetidaUnaSolaVez        25/10/2026 02:30 se repite en el reloj, se ejecuta una vez
PASS  TestZonasEmpotradas                   4 zonas IANA sin depender del mapeo de Windows
PASS  TestReglasInvalidas                   4 reglas mal formadas producen error
```

La propiedad «la hora repetida se ejecuta una sola vez» no depende de cómo resuelva la ambigüedad la biblioteca: sale de comparar instantes UTC de forma monótona. La prueba también verifica que no se repite la clave de idempotencia.

Código en `packages/scheduler/occurrence.go`.

## P-02 · Tarea de reactivación — verde parcial

### Fallo encontrado en la plantilla XML del plan

El primer intento devolvió **`Error: Acceso denegado`**. No era elevación ni `WakeToRun`: era que el `<Principal>` **no llevaba `<UserId>`**.

- Con `<UserId>ARGALLA\kirne</UserId>` explícito: **`Correcto`**, sin elevación, con `WakeToRun` incluido.
- Sin `UserId`: acceso denegado, con o sin `WakeToRun`.

La plantilla del plan usaba `%USERDOMAIN%\%USERNAME%`, que **schtasks no expande**: los toma como literales. El runner tiene que resolver la identidad real y escribirla en el XML.

Lo que hace peligroso este fallo es el mensaje: «Acceso denegado» empuja a pedir elevación, que es justo lo que no hay que hacer y lo que habría degradado el producto sin necesidad.

### Estado real de los temporizadores de reactivación

```
Permitir temporizadores de reactivación (alias RTCWAKE)
  Corriente alterna : 0x00000001  Habilitar
  Corriente continua: 0x00000000  Deshabilitar
```

Confirmado en la máquina del titular y con la configuración de fábrica: **con batería los temporizadores vienen desactivados**. El preflight que la especificación pedía no es una precaución teórica, es el caso por defecto de cualquier portátil.

Dos precisiones sobre lo que decía el plan:

1. **Es tri-estado, no booleano.** Los valores posibles son `0` Deshabilitar, `1` Habilitar y `2` *Solo temporizadores de activación importantes*. El valor `2` también impide que despierte una tarea nuestra, porque no es un temporizador de sistema. El preflight debe tratar `0` y `2` como «no despertará».
2. **Usar el alias `RTCWAKE`, no el GUID.** `powercfg /q SCHEME_CURRENT SUB_SLEEP RTCWAKE` es estable e independiente del idioma. Analizar el texto descriptivo no lo es: en esta máquina sale en castellano y con acentos.

### Modern Standby

```
Estados disponibles: Modo de espera (Inactivo de baja energía S0) Red conectada · Hibernar · Inicio rápido
Estados NO disponibles: Modo de espera (S1), (S2), (S3)
```

La máquina no tiene suspensión clásica S3: usa **Modern Standby (S0 Low Power Idle) con red conectada**. Es el modo habitual de los portátiles recientes y cambia el modelo mental: el sistema no se «apaga», mantiene actividad reducida. Falta medir si en S0 la tarea programada se ejecuta a su hora sin necesidad de temporizador de reactivación, lo que haría el problema menos grave de lo previsto en máquinas modernas.

### Lo que falta

La prueba de suspender el equipo y comprobar que despierta y ejecuta **requiere suspender la máquina del titular**, así que queda como paso manual acordado. Sin ella, P-02 no está cerrada del todo.

## Codex · Protocolo verificado a nivel de esquema

`codex app-server generate-json-schema --out <dir>` **emite el esquema JSON completo del protocolo**: 39 ficheros, 95 variantes de petición.

Métodos confirmados, más de los que documentaba la ficha:

```
thread/start   thread/resume  thread/fork    thread/read     thread/list
thread/archive thread/delete  thread/rollback thread/compacted thread/shellCommand
turn/start     turn/interrupt turn/steer     turn/started    turn/completed
```

Detalles con consecuencias de producto:

- **`thread/fork` acepta `lastTurnId`**, «inclusive»: se puede derivar desde un punto concreto de la conversación, no solo desde el final. La especificación asumía lo segundo. Es una capacidad mejor de la esperada.
- **`turn/interrupt` necesita `threadId` y `turnId`**, no solo el hilo. El runner tiene que guardar el identificador del turno en curso para poder cancelar limpiamente.
- **`turn/start` admite `effort` y `cwd` por turno**, lo que permite ajustar esfuerzo por tarea sin tocar la configuración global del usuario.
- **`thread/start` admite `ephemeral`**, un hilo que no se persiste. Encaja con la sección 32 de privacidad.
- **`thread/list` filtra por `cwd`, `archived`, `modelProviders`, con `cursor` y `limit`.** Cubre FR-003 sin tocar ficheros internos.

Consecuencia para D2: el contrato de Codex **no hay que escribirlo a mano**. Se genera del esquema que publica el propio proveedor, y regenerarlo tras cada actualización es la forma de detectar un cambio de protocolo antes de que lo detecte un usuario a las tres de la mañana.

Salvedad: `app-server` sigue marcado `[experimental]` en la ayuda del binario, y la versión instalada es una alfa. La degradación a `codex exec` que ya contempla el plan deja de ser una precaución de manual y pasa a ser una ruta que habrá que ejercitar de verdad.

---

## Correcciones que estos resultados obligan a hacer

1. **FR-001**, ficha §11: la detección se hace por patrón dentro de `~/.vscode/extensions`, con orden de versión, y se resuelve en cada preflight. No se guarda la ruta como si fuera estable.
2. **Plantilla de la tarea programada**, plan §8.3: `<UserId>` resuelto y escrito, nunca variables de entorno. Documentar que su ausencia se manifiesta como «Acceso denegado».
3. **Comprobación de temporizadores**, plan §8.3: usar el alias `RTCWAKE` y tratar el valor `2` como «no despertará».
4. **Adaptador de Codex**, ficha §17 y plan §10.2: generar los tipos del esquema del proveedor; guardar `turnId` para poder interrumpir; aprovechar `lastTurnId` en el fork.
5. **Matriz de versiones**, ficha §21: Codex instalado es una alfa y `app-server` es experimental.

---

## Adenda · ¿Hace falta suspender el equipo para cerrar P-02?

**No, y en esta máquina además sería poco concluyente.**

P-02 tenía que demostrar tres cosas. Dos ya están demostradas sin suspender nada:

1. Que la tarea se registra con `WakeToRun` y sin elevación → **probado**.
2. Que los temporizadores de reactivación dependen de la fuente de alimentación → **probado**: AC=1, DC=0.
3. Que el equipo despierta y ejecuta → no probado.

Sobre lo tercero, dos datos:

- `powercfg /lastwake` devuelve **«Recuento de historial de activación - 0»**: esta máquina no ha despertado nunca de una suspensión. `powercfg /waketimers`, que listaría los temporizadores armados, **exige elevación** y no sirve como comprobación desde el producto.
- La máquina usa **Modern Standby (S0) con red conectada y no tiene S3**. En S0 el sistema no se suspende del todo: sigue funcionando a bajo consumo y el Programador de tareas puede disparar sin necesidad de un despertar por RTC. Es decir, **medir aquí diría poco sobre las máquinas con S3**, que son las que de verdad dependen del mecanismo que P-02 quiere validar.

Decisión: **no se fuerza una suspensión**. En su lugar queda instalada una sonda pasiva:

```
Tarea:  ACWakeProbe   ·  diaria 03:15  ·  WakeToRun activado
Acción: anota fecha y hora en docs/fase-0/wake-log.txt
Borrar: schtasks /Delete /TN ACWakeProbe /F
```

Si alguna noche el equipo entra en reposo por sí solo, el registro lo dirá sin intervención. Cada línea del fichero es una ejecución nocturna real.

**Cuándo sí será imprescindible:** en la Fase 4, con un runner de verdad al que despertar y sobre una máquina con S3, idealmente de un probador externo con hardware más antiguo. Forzar una suspensión ahora solo demostraría que `cmd.exe` se ejecuta.


---

## P-03 · Listado de sesiones de Claude — verde

`listSessions` del Agent SDK devuelve **95 sesiones reales** de esta máquina. Campos disponibles:

```
sessionId · summary · customTitle · firstPrompt · gitBranch · cwd · tag · createdAt · lastModified · fileSize
```

Más de lo previsto: `gitBranch` y `tag` sirven directamente para el selector, y `firstPrompt` para la vista previa sin abrir el transcrito.

Dos comprobaciones que evitan trabajo inútil:

1. **El SDK normaliza la ruta.** `C:\Users\kirne`, `c:\users\kirne` y `C:/Users/kirne` devuelven las mismas 95 sesiones. La extensión no tiene que normalizar mayúsculas ni barras.
2. **El SDK no devuelve el directorio codificado.** No hay campo `projectDir`. Como el runner necesita esa ruta para validar que el transcrito sigue existiendo, se resuelve **buscando el fichero por patrón** una sola vez:

   ```
   ~/.claude/projects/*/<sessionId>.jsonl
   ```

   Verificado: la sesión `1bce5e08…` aparece en `C--Users-kirne/`. Buscar el fichero es más robusto que reimplementar la regla de codificación, que además trunca y añade un hash a partir de 200 caracteres. En esta máquina la carpeta más larga ya mide 118.

   Detalle: conviven `C--Users-kirne` y `c--Users-kirne-Desktop-Apps-peneira`. La carpeta **conserva la caja del `cwd` con que se creó la sesión**, así que la búsqueda por patrón debe ser insensible a mayúsculas.

## P-04 · Aislamiento del canal — verde

Named Pipe con descriptor de seguridad explícito `D:P(A;;GA;;;<SID>)`, sobre `go-winio`.

```
canal: \\.\pipe\Argalla.AgentCalendar.S-1-5-21-…-1001
PASS  TestCanalSoloParaElUsuario        ida y vuelta correcta para el propietario
PASS  TestCanalNoAdmiteDosServidores    el segundo servidor recibe "Acceso denegado"
PASS  TestNoSeAbrePuertoTCP             la dirección no es TCP: winio.pipeAddress
```

Tres consecuencias:

1. **No hay puerto TCP**, así que la superficie que motivó descartar HTTP en `127.0.0.1` —una página abierta en el navegador del usuario hablando con el runner— sencillamente no existe.
2. **El nombre del canal no admite dos servidores.** Es una segunda barrera de instancia única, gratis, además del mutex. Se conservan las dos: el mutex protege del solapamiento entre procesos aunque el canal aún no esté abierto.
3. El SID en el nombre evita que dos sesiones simultáneas de Windows compartan canal.

Recordatorio honesto: la frontera es la **cuenta de usuario**. Otro proceso del mismo usuario puede conectarse, y ningún secreto compartido lo evitaría porque ese proceso también podría leerlo. Es la misma frontera que protege las credenciales de los propios CLI.

## P-06 · Cuota y credencial — verde con hallazgo grave

Provocado en un entorno aislado (`CLAUDE_CONFIG_DIR` a un directorio vacío y clave inválida), sin tocar las credenciales reales.

### 1. La señal es estructurada, no un mensaje de texto

El fallo de autenticación **no hay que buscarlo con expresiones regulares en la salida de error**. Llega como evento del flujo:

```json
{"type":"system","subtype":"api_retry","attempt":8,"max_retries":10,
 "retry_delay_ms":38052,"error_status":401,"error":"authentication_failed"}
```

Esto mejora el diseño: `error_status` y `error` son campos, no prosa traducible. La tabla de patrones de texto pasa a ser el último recurso, no el primero.

### 2. **Una credencial caducada no falla: se reintenta durante minutos**

Diez reintentos con espera creciente. En la sonda, el intento 8 esperaba **38 segundos** antes del siguiente. La ejecución agotó el tiempo límite de 90 segundos sin llegar a terminar por sí sola.

Consecuencia directa para el runner, que el plan no contemplaba: **al primer `api_retry` con `error_status: 401` hay que cortar el proceso y marcar `failed_auth`.** Si no, cada tarea con la credencial caducada consume su timeout completo, y una tarea nocturna con timeout de una hora se pasa la noche reintentando contra una pared.

### 3. El modo de permisos por defecto era `bypassPermissions`

Sin pasar `--permission-mode`, el proceso hijo arrancó con **`bypassPermissions`**, el modo que la especificación prohíbe. No venía del entorno: lo aportaba la configuración del propio usuario, que además tenía 16 entradas de permisos concedidos.

Verificado que la defensa funciona:

```
--permission-mode default --disallowedTools "Bash" "Edit" "Write"
  → permissionMode = default
  → Bash, Edit y Write ausentes de la lista de herramientas (22 restantes)
```

Pero la lección es que **el perfil de seguridad no puede confiar en los valores por defecto**: tiene que fijar el modo explícitamente en cada lanzamiento, y además **neutralizar la configuración ambiente del usuario**, que puede conceder más de lo que el perfil pretende.

Cada proveedor tiene su mecanismo, y los dos hay que usarlos:

| Proveedor | Neutralizar configuración ambiente |
|---|---|
| Claude Code | `--settings <fichero controlado>` |
| Codex | `--ignore-user-config` |

### 4. Espacio de trabajo no confiado

```
Ignoring 16 permissions.allow entries from .claude/settings.json:
this workspace has not been trusted.
```

Un proyecto que nunca se ha abierto de forma interactiva **no es de confianza**, y sus permisos se ignoran. Para una tarea programada sobre un repositorio recién clonado, eso significa que el agente arranca con menos permisos de los que la tarea necesita, y falla por una razón que no tiene nada que ver con la tarea.

El preflight tiene que comprobarlo y decirlo con claridad, con la salida documentada a mano: `projects["<ruta>"].hasTrustDialogAccepted` en `.claude.json`.

### 5. Codex: `-a` no existe en `codex exec`

```
error: unexpected argument '-a' found
```

**Corrección a la ficha y al plan**, que daban `-a never` por obligatorio. `--ask-for-approval` es del modo interactivo. `codex exec` no pregunta porque no es interactivo, y sus opciones reales son otras:

| Opción | Uso en el producto |
|---|---|
| `-s, --sandbox` | `read-only` en auditoría, `workspace-write` en el worktree |
| `--ignore-user-config` | Neutraliza la configuración del usuario. **Obligatoria en el perfil de seguridad** |
| `--add-dir` | Directorios adicionales escribibles |
| `--ephemeral` | No persiste la sesión en disco. Encaja con privacidad |
| `--skip-git-repo-check` | Solo para proyectos sin Git, en modo lectura |
| `--dangerously-bypass-approvals-and-sandbox` | **Prohibida** |

Invocación correcta para una tarea programada de solo lectura:

```
codex exec --json --cd <dir> -s read-only --ignore-user-config "<prompt>"
```

---

## Correcciones adicionales que estos resultados obligan a hacer

6. **Clasificación de errores**: leer `error_status` y `error` del evento `api_retry`, no analizar texto. Cortar al primer 401.
7. **Perfiles de seguridad**: `--permission-mode` explícito siempre, más `--settings` en Claude y `--ignore-user-config` en Codex.
8. **Preflight**: comprobar que el espacio de trabajo es de confianza antes de programar una tarea con escritura.
9. **Adaptador de Codex**: quitar `-a never`, que no existe en `exec`.
10. **Resolución del transcrito**: buscar `~/.claude/projects/*/<id>.jsonl` sin distinguir mayúsculas, en vez de recalcular la codificación.
