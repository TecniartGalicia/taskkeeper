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
| P-03 Listado de sesiones de Claude | ⏳ Pendiente | |
| P-06 Cuota y credencial | ⏳ Pendiente | |
| P-04 Aislamiento del canal | ⏳ Pendiente | |
| Condiciones de uso | ⏳ Pendiente | Informe antes de la Fase 1 |

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
