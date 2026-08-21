# TaskKeeper — Plan de marketing

Estado del producto: **v0.8.5 publicada** en las tres tiendas (VS Marketplace, Open VSX)
para **Windows x64 y macOS (Apple Silicon + Intel)**. Gratis, local-first, sin telemetría.
Editor/publisher: **argalla** (Tecniart Galicia, S.L.). Repo: `TecniartGalicia/taskkeeper`.

> Regla de casa: **los posts los redacto yo, los publica una persona.** Nada de automatizar
> publicaciones en redes (X suspende cuentas por eso). Los borradores están en la §7.

**Estado del lanzamiento (2026-08-21):**
- **HECHO (canales propios, sin riesgo):** publicado en VS Marketplace + Open VSX (Win + macOS firmado);
  **GitHub Release v0.8.7** reescrito como anuncio (GIF + diferenciador + instalación + novedades);
  repo con descripción, homepage → Marketplace, y 12 *topics* para descubrimiento.
- **PENDIENTE (los publica una persona):** Show HN, dev.to, Reddit, X — borradores listos en la §7.
  Puedo dejar cada uno cargado en su formulario para que solo revises y pulses publicar.

---

## 1. Posicionamiento (una frase)

**El turno de noche para tus agentes de código, aislado y revisable.**

Programar prompts ya lo hacen varias herramientas —hasta los propios agentes—. Lo que **nadie
más** hace es mantener el trabajo del agente **aislado en un worktree de Git y detrás de una
revisión**. Todos los demás programadores ejecutan el agente sobre tu directorio de trabajo:
si mete la pata a las 3 de la mañana, la mete sobre tu código. TaskKeeper la mete sobre una copia,
y por la mañana tú decides con el diff delante.

Ese es el *wedge*: **no competimos en “programar”, competimos en “aislar y revisar”.**

## 2. Público objetivo

- **Núcleo:** desarrolladores que **ya usan Claude Code o Codex** a diario (power users, early adopters).
- **Trabajos que resuelve de un vistazo** (esto es lo que se vende, no la feature):
  - Subir dependencias y dejar el diff listo para revisar.
  - Mantener el changelog al día.
  - Barrido de lint / formateo nocturno.
  - Arreglar tests que fallan mientras duermes.
  - Auditorías recurrentes de solo lectura (seguridad, TODOs, deuda).
  - Refactors largos que no quieres ver correr en directo.
- **Dónde están:** búsqueda del Marketplace, Hacker News, r/ClaudeAI, r/ChatGPTCoding,
  r/vscode, dev.to, X (comunidad “AI coding”), Discord/Slack de Claude Code y Codex.

## 3. Mensaje y diferenciador

- **Titular:** *Your coding agents work the night shift — isolated and reviewable.*
- **La frase que se queda:** *“If it gets it wrong at 3 a.m., it gets it wrong on a copy.”*
- **Prueba, no adjetivos:** worktree por ejecución + bandeja de revisión (aceptar = merge local,
  nunca push; rechazar = borra worktree y rama). Dos perfiles de permisos explícitos en cada
  ejecución; no confiamos en los defaults del agente (los medimos, eran demasiado laxos).
- **Confianza:** local-first, sin telemetría, sin cuenta, sin llamadas de red propias. Corre con
  VS Code cerrado usando el planificador del sistema operativo (no un demonio nuestro).
- **Qué NO decir:** no prometer notarización de Apple (los binarios van firmados con Developer ID;
  la extensión quita la cuarentena). No dar a entender afiliación con Anthropic ni OpenAI.

## 4. Activos de la store (lo que más convierte)

Prioridad por impacto en instalaciones:

1. **[HECHO] README alineado con la realidad** — corregido el fallo de que decía “solo Windows,
   macOS sin verificar” cuando macOS **ya está publicado**. Añadidas las features nuevas
   (resumen de anoche + salud del planificador, gasto + tope mensual, plantillas, encadenado,
   tutorial).
2. **[HECHO] Metadatos** — keywords ampliadas, quitado el flag `preview` (Windows es maduro desde
   0.1.0; el matiz de macOS-preview va en el texto, no marcando toda la extensión como preview),
   Q&A del marketplace activo.
3. **[HECHO en 0.8.7] Capturas y GIF.** Cuatro capturas + un GIF, generados renderizando el **HTML
   real de los webviews** (extraído de los `.ts`) con el tema Dark Modern de VS Code inyectado y datos
   de ejemplo, dentro de un marco de pestaña de VS Code — fieles al panel real, no maquetas. En
   `media/shots/` (excluidas del VSIX por `.vscodeignore`; servidas en el README por URL raw de GitHub):
   - `inbox.png` — **hero**: la bandaja de la mañana (ejecución terminada + aceptar/rechazar + transcripción).
   - `new-task.png` — crear tarea, intención primero (plantillas + los dos ejes + repo + agente).
   - `digest.png` — «Anoche» + salud del planificador (fila roja «Trigger missing»).
   - `spend.png` — gasto del mes vs. tope, por día y por tarea.
   - `demo.gif` — flujo: corre de noche → termina → espera revisión → aceptado (8 s, 60 KB, en bucle).
   - Scripts reproducibles: `~/handsfree-browser/mkshots.mjs` (capturas), `mkgif.mjs` (fotogramas del
     GIF) y `mktask.mjs` (panel de tarea); el GIF se monta con ffmpeg (concat + palette).
4. **Descripción corta** afinada para búsqueda + valor (ver §5).

## 5. Copys listos (store)

**Short description (la de la búsqueda):**
> Schedule Claude Code and Codex to run overnight in an isolated Git worktree — review the diff in
> the morning before anything touches your branch. Local-first, no telemetry.

**Keywords (orden = prioridad; el Marketplace muestra las primeras como etiquetas):**
`claude code`, `codex`, `ai agent`, `scheduler`, `git worktree`, `cron`, `code review`, `diff`,
`unattended`, `overnight`, `background agent`, `anthropic`, `openai`, `automation`, `night shift`.

## 6. Plan de captura de imágenes (la tarea pendiente)

Receta en `~/…/reference_grabar_demo_vscode.md` (VS Code guionizado + captura de ventana + ffmpeg).
Pasos concretos para TaskKeeper:

1. Preparar un repo de demo pequeño y **datos sembrados** en la BD de TaskKeeper: una ejecución
   `awaiting_review` con un diff bonito, un par de tareas con horario, y algo de gasto para que el
   panel no salga vacío. (Script sugerido: sembrar vía `taskkeeper-ctl` o directamente en el SQLite
   de un `TASKKEEPER_HOME` de demo, sin tocar el real.)
2. Tema claro de VS Code + zoom de UI a ~1.2 para que se lea. Ocultar barras que distraigan.
3. Capturar 5 PNG (hero + 4) a 1280×800 o 1456×916. Guardar en `apps/vscode-extension/media/shots/`.
4. Grabar el GIF (crear→ejecutar→diff→aceptar) con ffmpeg, ≤ 2 MB, bucle.
5. Insertar en el README bajo `## Screenshots` (la sección ya está preparada con marcadores).
6. Publicar 0.8.x con las imágenes.

> Nota: es lo único de este plan que necesita “manos” (sembrar datos + grabar). El resto ya está
> ejecutado en el repo.

## 7. Lanzamiento — borradores (redacto yo, publica una persona)

Orden recomendado: **Show HN** primero (mayor alcance técnico), luego dev.to (contenido perenne),
luego Reddit y X. Escalonado en 3–4 días, no todo el mismo día.

### 7.1 Show HN
**Título:** `Show HN: TaskKeeper – Run Claude Code/Codex overnight in an isolated git worktree`

**Texto:**
> I use Claude Code and Codex a lot, and I kept wanting to hand them a task at night — bump
> dependencies, fix the flaky test, keep the changelog current — and just review it in the morning.
> The agents (and a couple of extensions) can already be scheduled, but they all run on your working
> directory. If the agent gets something wrong at 3 a.m., it gets it wrong on your code.
>
> TaskKeeper runs the agent inside a **fresh git worktree** from the exact base commit, and drops the
> result into a review inbox: the diff in VS Code's own diff editor, the files it changed, what it
> cost, and two buttons — accept (local merge, never a push) or discard (worktree and branch gone).
>
> No daemon: the OS scheduler (Windows Task Scheduler / macOS launchd) launches a short-lived worker;
> state is a local SQLite file. Local-first, no telemetry, no account, no network call of its own.
> Two explicit permission profiles per run (read-only audit / isolated changes) — it doesn't trust
> the agent's defaults. Free. Windows and macOS.
>
> Not affiliated with Anthropic or OpenAI. Happy to answer anything.

### 7.2 dev.to / blog (perenne)
**Título:** `I gave my AI coding agent a night shift`

**Esqueleto:**
1. El problema: quiero delegar tareas de madrugada, pero no quiero que toque mi rama.
2. Por qué “programar el prompt” no basta (corre sobre tu working dir).
3. La idea: worktree por ejecución + bandeja de revisión por la mañana.
4. Recorrido con capturas (crear tarea → horario → diff → aceptar).
5. Cómo corre con VS Code cerrado (planificador del SO, sin demonio) y por qué eso importa.
6. Seguridad: dos perfiles de permisos, sin telemetría, todo local.
7. Tres recetas: dependencias, changelog, tests flaky.
8. Cierre + enlace a la store.

### 7.3 Reddit (r/ClaudeAI, r/ChatGPTCoding, r/vscode)
Post value-first, sin tono de anuncio:
> **Made a VS Code extension so Claude Code / Codex can work overnight without touching my branch**
> Every scheduler I found runs the agent on your working directory. This one runs it in a throwaway
> git worktree and shows you the diff in the morning to accept or discard. Local-first, no telemetry,
> free. Would love feedback on the review flow. [enlace]

### 7.4 X / thread (3–4 tuits)
1. Le puse turno de noche a mis agentes de código. Claude Code y Codex corren de madrugada
   **en un worktree aislado**, y por la mañana reviso el diff y decido. VS Code cerrado.
2. La diferencia con “programar un prompt”: los demás corren sobre tu working dir. Si el agente se
   equivoca a las 3am, se equivoca sobre tu código. Aquí se equivoca sobre una copia.
3. Aceptar = merge local (nunca push). Rechazar = borra worktree y rama. Dos perfiles de permisos
   por ejecución. Local-first, sin telemetría, gratis. Windows y macOS. [enlace]

## 8. Calendario (2 semanas)

- **Semana 1 — activos:** [hecho] README + metadatos; [pendiente] capturas + GIF → publicar la
  versión con imágenes.
- **Semana 2 — lanzamiento:** Día 1 Show HN (mañana EEUU). Día 2 dev.to. Día 3 Reddit. Día 4 X.
  Responder todos los comentarios el mismo día (el flujo de revisión es lo que preguntarán).

## 9. Métricas

- Marketplace: instalaciones, rating, preguntas en Q&A.
- GitHub: stars, issues abiertas (señal de uso real).
- Lanzamiento: posición en HN, upvotes/comentarios en Reddit, clics a la store.
- Objetivo realista primer mes: primeras reseñas + issues de macOS en hardware real (que es lo que
  falta por validar del 0.8.4/0.8.5).

## 10. Decisiones abiertas (para hablar)

- **Changelog en inglés.** La ficha es en inglés pero el CHANGELOG está en castellano. Para alcance
  internacional conviene inglés (o bilingüe). Baja prioridad frente a las capturas.
- **Precio.** Hoy **gratis** (mejor para crecer y captar reseñas). Un Pro más adelante (como
  ChangeKeeper/SessionKeeper) es opción, pero primero tracción.
- **Vídeo de 60–90 s** en YouTube: útil pero opcional; el GIF de la ficha cubre lo esencial.
