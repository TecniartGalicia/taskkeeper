# Changelog








## 0.8.4 — 2026-08-20

- **macOS operativo (preview).** Los binarios de Mac se publican **firmados con Developer ID**; la extensión, al instalarlos, les da permiso de ejecución y quita el atributo de cuarentena para que se abran sin bloqueos. (Se deja la notarización de Apple para más adelante: con binarios sueltos se quedaba «en proceso» más allá del límite del CI.)

## 0.8.3 — 2026-08-20

- Primera publicación con las tres plataformas: Windows y **macOS (Apple Silicon + Intel)**, con los binarios firmados y notarizados. (0.8.2 salió solo para Windows por un fallo de firma en el CI, ya corregido.)

## 0.8.2 — 2026-08-20

- **macOS (preview).** TaskKeeper empieza a publicarse también para Mac (Apple Silicon e Intel). Los binarios van **firmados con Developer ID y notarizados por Apple**, así que se abren sin avisos de Gatekeeper. Sigue en vista previa mientras se verifica el comportamiento en hardware real; los informes de usuarios de Mac son bienvenidos.

## 0.8.1 — 2026-08-20

- Pulido tras la auditoría final: al encadenar «cuando termine bien», ahora se avisa y se impide elegir una tarea padre aislada (que nunca se aceptaría sola y dejaría la dependencia muerta); usa «cuando falle» o «de cualquier modo», o haz que el padre corra en la conversación. Los paneles de resumen y gasto ya no dejan promesas sin capturar si el agente falla durante un refresco automático.

## 0.8.0 — 2026-08-20

- **Encadenar tareas / acción ante fallo.** Una tarea puede ejecutarse **después de otra**: en el panel (avanzado) eliges «se ejecuta tras…» una tarea y cuándo —cuando termine bien, cuando falle, o de cualquier modo—. La tarea dependiente no tiene horario propio: la dispara su padre al terminar. Se validan los ciclos (A→B→A se rechaza) y en la lista de tareas se ve «tras «Padre»». v1: el encadenado por «termina bien» requiere que el padre corra en la conversación (directo); un padre aislado encadena de momento solo en fallo (el éxito por «aceptar» llegará después).

## 0.7.0 — 2026-08-20

- **Plantillas de tarea.** Al crear una tarea, el panel ofrece un catálogo de tareas típicas —actualizar dependencias, redactar changelog, barrido de lint, arreglar tests que fallan— que precargan el nombre, el prompt, los permisos, el modo y el horario de un clic; luego ajustas lo que quieras. «Ninguna» vuelve al andamio genérico. Las que cambian código van siempre en modo aislado (revisable); el changelog es de solo lectura.

## 0.6.1 — 2026-08-20

- **Gasto visible + tope mensual.** Un panel nuevo («Gasto», desde la bandeja) muestra el coste del mes frente a un **tope mensual** que puedes fijar, con desglose por día (barras) y por tarea. El tope lo respeta el propio ejecutor: si el mes ya llegó al tope, la siguiente tarea se **salta** antes de gastar (aviso con el motivo). También se aplica ya el **tope diario por tarea** (antes se guardaba pero no surtía efecto). Nota: el coste solo lo informa Claude; con Codex el gasto no se contabiliza y su tope no tiene efecto — el panel lo advierte.

## 0.6.0 — 2026-08-20

- **Resumen de anoche + salud del planificador.** Un panel nuevo («Anoche», también desde la bandeja) resume de un vistazo lo que corrió mientras no estabas —terminadas, esperando revisión, fallidas, saltadas, en curso y coste— y, sobre todo, detecta el **fallo silencioso**: si el disparador de una tarea en el sistema operativo se ha desregistrado, lo marca en rojo («falta el disparador»); también cuenta los disparos perdidos por retraso. Se abre solo una vez al día, al lado y sin robar el foco, si hubo actividad.

## 0.5.0 — 2026-08-20

- **Crear una tarea empieza por la intención.** El panel abre con dos atajos —**«En una conversación»** (continúa una conversación; el trabajo queda en su chat) y **«Tarea aislada en un repo»** (worktree que revisas)— que fijan por ti los dos ejes (conversación × dónde trabaja). Con «En una conversación» el repositorio lo pone la propia conversación (autodetección de carpeta), y sobra elegirlo a mano. Un enlace **«Avanzado»** revela los dos ejes por separado para mezclarlos (p. ej. continuar + aislada).
- **Tutorial interactivo (ES/EN).** La primera vez que se instala, o desde **«Empezar (tutorial)»**, se abre un recorrido guiado de cuatro pasos —los dos ejes, crear la tarea, el horario y ver el resultado— en el idioma de VS Code. Iconos propios, sin emojis.

## 0.4.0 — 2026-08-19

- **La carpeta se detecta sola desde la conversación.** Al elegir «Continuar» o «Derivar» y meter el id de una conversación, TaskKeeper averigua en qué carpeta vive esa conversación y pone el repositorio por ti — ya no tienes que acertar la carpeta a mano. Si la misma conversación existe en varias carpetas, te las ofrece para elegir.

## 0.3.3 — 2026-08-19

- La vista de resultado muestra la **carpeta** donde corrió la ejecución.
- **«Abrir en la conversación»** ahora, si esa carpeta no es la que tienes abierta en VS Code, te avisa y te ofrece **abrir la carpeta correcta** (y al recargar abre la conversación sola), en vez de una ventana de chat vacía. Recuerda: para continuar una conversación, el repositorio de la tarea debe ser la carpeta donde vive esa conversación.

## 0.3.2 — 2026-08-19

- **«Abrir en la conversación».** Cuando una tarea corre «en la conversación» (modo directo continuando una conversación), Claude Code escribe todo el intercambio en el fichero de esa conversación. La vista de resultado añade un botón **«Abrir en la conversación»** que salta al chat nativo de Claude Code, para verlo y seguirlo como una interacción normal. La transcripción de TaskKeeper se conserva para revisar.

## 0.3.1 — 2026-08-19

- **«En la conversación» ya no exige un repositorio Git.** Para continuar una conversación en modo directo basta con elegir la carpeta donde vive (sigue haciendo falta para localizar la conversación y como directorio de trabajo del agente), pero ya no tiene que ser un repo Git ni tener una rama base. El modo aislado sigue exigiendo Git como debe, y cambiar una tarea a aislado sobre una carpeta sin Git se rechaza al editar, no al ejecutar.

## 0.3.0 — 2026-08-19

Resultados legibles y ejecución «en la conversación».

- **Resultado como transcripción, no como volcado JSON:** al abrir una ejecución ves una conversación legible —lo que dijo el agente, los comandos que ejecutó (con su salida plegable) y la respuesta final— con una cabecera de resumen (estado, coste, vueltas, ficheros) y botones de aceptar/rechazar/archivar. Se actualiza en vivo mientras corre. El registro crudo sigue disponible en «TaskKeeper: Ver ejecución (registro crudo)».
- **Ejecutar en la conversación (opcional):** una tarea puede correr **en el repositorio real** en vez de en un worktree aislado, añadiendo sus turnos a la conversación que elijas —como si lo hubieras escrito tú. Ideal para comprobaciones recurrentes de solo lectura. En cambios, se convierte en «cambios directos» con confirmación explícita (por defecto todo sigue aislado y revisable).

## 0.2.0 — 2026-08-19

Panel visual para configurar una tarea, en lugar de los siete diálogos.

- **Un panel para crear y editar la tarea:** todos los campos a la vista y editables en cualquier orden, con resumen lateral. El asistente de 7 pasos sigue disponible como «Nueva tarea (rápida)», y para editar hay «Editar tarea».
- **Varias horas por tarea:** p. ej. todos los días a las 15:00 y a las 20:00 en una sola tarea, con la «próxima ejecución» calculada en vivo.
- **Editar en el mismo panel:** abre una tarea existente y cambia lo que quieras (el repositorio queda fijo).
- **Compactar conversación larga (Claude Code):** opción por tarea para fijar la ventana de autocompactación (`--autocompact`). Claude Code ya compacta solo al acercarse al límite; esto solo da un umbral predecible para conversaciones muy largas.

## 0.1.1 — 2026-08-19

Corrección de un fallo encontrado en uso real:

- **Continuar/derivar una conversación ya funciona.** En 0.1.0 una tarea con «Resume» o «Fork» arrancaba el agente sin el id de la conversación (`--resume` vacío) y fallaba al instante, sin gastar nada. Ahora el runner resuelve el id de sesión guardado y lo pasa al agente; si la tarea no tiene una referencia de sesión válida, falla con un motivo claro (`sin_referencia_sesion`) en vez del error críptico del agente.

## 0.1.0 — 2026-08-19 (pre-release)

First public release. Windows x64.

- Schedule Claude Code or Codex tasks (daily, weekly, once) registered with Windows Task Scheduler; runs with VS Code closed.
- Every run in its own Git worktree from the resolved base commit; the main checkout is never touched.
- Morning inbox: diff in VS Code's editor, files changed, cost; accept (local merge, no push) or reject (worktree and branch removed).
- New / resume / fork conversation modes for Claude Code; new for Codex.
- Two permission profiles applied explicitly on every run; agent defaults are never trusted.
- Per-run spending cap; provider quota and expired sign-in recognised and reported instead of hanging.
- Machine-wide concurrency slot (default 1), cancellation that kills the whole process tree, missed-run policy per task.
- Full local audit trail. English and Spanish UI.
