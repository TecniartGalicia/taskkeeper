# Análisis de competencia

TaskKeeper (antes Agent Calendar) · Argalla · 18 de agosto de 2026
Datos tomados del Marketplace de VS Code y de la documentación de los proveedores el mismo día.

---

## Respuesta a la pregunta

**No, nuestra propuesta no es muy superior. En lo que creíamos que era el producto, vamos por detrás.**

La promesa central de la ficha —«programa tareas para tu agente y que se ejecuten aunque VS Code esté cerrado»— **ya la cumplen tres cosas gratuitas**, una de ellas del propio fabricante. Nueve semanas de trabajo para llegar segundos a un sitio donde ya se llega gratis no es un plan.

Ahora bien, hay una parte de la propuesta que **nadie ofrece, ni siquiera Anthropic**, y que resulta ser la más valiosa. La conclusión no es abandonar: es **dejar de vender el calendario y empezar a vender lo otro**.

---

## 1. El competidor más peligroso es Anthropic

Claude Code ya trae **tres** mecanismos de programación, gratis con la suscripción:

| | Rutinas en la nube | **Tareas de escritorio** | `/loop` en sesión |
|---|---|---|---|
| Requiere el equipo encendido | No | Sí | Sí |
| **Requiere sesión abierta** | No | **No** | Sí |
| **Persiste tras reiniciar** | Sí | **Sí** | Solo al reanudar |
| Acceso a ficheros locales | No | **Sí** | Sí |
| **Permisos** | Autónomo | **Configurables por tarea** | Los de la sesión |
| Intervalo mínimo | 1 hora | **1 minuto** | 1 minuto |

Léase la columna del medio despacio. **Las tareas de escritorio de Claude Code ya hacen: ejecutar con el editor cerrado, sobrevivir a reinicios, acceder a los ficheros locales y aplicar permisos por tarea.** Es, casi literalmente, la lista de la sección 7 de nuestra ficha.

Y `/loop` va más allá de lo que teníamos previsto: acepta lenguaje natural («recuérdame a las tres que suba la rama»), elige el intervalo por sí mismo según lo que observa, y admite un `loop.md` por proyecto. Nuestro «asistente de siete pasos» compite contra escribir una frase.

Codex tiene sus propias automatizaciones. Los dos fabricantes cubren su propio terreno, gratis.

## 2. Y hay un competidor directo publicado hace siete días

`Adza.claude-code-scheduler` — *Prompt Scheduler for Claude Code* — v0.3.0, MIT, gratis, publicada el **11 de agosto de 2026**.

Lo que ya tiene resuelto, según su propia documentación:

- **Linux, macOS y Windows.** Nosotros salimos solo con Windows.
- **Se apoya en el programador del sistema operativo** —cron o el Programador de tareas de Windows— en vez de mantener un demonio propio. Una tarea del sistema por trabajo, agrupadas en su propia carpeta, sin tocar nada que no haya creado ella.
- **Bloqueo por trabajo** para que dos ejecuciones no se pisen.
- **Timeout configurable** que mata la ejecución colgada.
- **Estado real de cada ejecución**, incluidas las que ocurrieron con VS Code cerrado.
- **Detección del CLI** aunque solo esté en el PATH del shell de login.
- **El problema del espacio de trabajo no confiado**, que yo encontré ayer en la Fase 0 y anoté como hallazgo: ellos lo documentan como requisito y ponen un botón **«Verify Setup»** para resolverlo antes de guardar la tarea.

Tiene 4 instalaciones porque tiene una semana, no porque haya fracasado.

## 3. El resto del terreno

| Extensión | Inst. | Estado | Qué es |
|---|---:|---|---|
| `KyleHoskins.roo-scheduler` | 1.151 | **Abandonada** (últ. cambio 05-2025) | Programador para Roo Code, no para Claude ni Codex |
| `Freaxys.better-cron-tasks` | 254 | **Abandonada** (08-2024) | Programador genérico de tareas de VS Code |
| `jjumman.copilot-prompt-scheduler` | 54 | **Viva**, actualizada hace 3 días | Copilot Chat + «soporte Claude Code». Cron, plantillas, historial, etiquetas, límites diarios, 3 idiomas |
| `mitgh.terminal-scheduler` | 11 | Viva (07-2026) | **Envía pulsaciones** a terminales integradas |
| `LongBanner.ai-agent-collab-scheduler` | 4 | Viva (06-2026) | **Codex + Claude en un panel**: nuestra tesis multiproveedor, ya intentada |
| `Adza.claude-code-scheduler` | 4 | **Nueva** (08-2026) | Ver arriba |

Corrijo lo que le dije ayer: **el nicho no está vacío y quieto. Está recién poblado.** Tres de los seis se publicaron o actualizaron en las últimas ocho semanas, y dos apuntan justo a nuestro posicionamiento.

---

## 4. Dónde sí somos superiores

Nada de lo anterior invalida el producto. Invalida **el argumento de venta**. Esto es lo que nadie tiene, incluidos los fabricantes:

| Capacidad | Nosotros | Anthropic nativo | Adza | jjumman |
|---|:--:|:--:|:--:|:--:|
| **Worktree aislado por ejecución** | **Sí** | No | No | No |
| **Reanudar o derivar una conversación existente** | **Sí** | No¹ | No | No |
| **Revisión con diff, tests y aceptar/rechazar** | **Sí** | No | No | No |
| **Un calendario para Claude y Codex a la vez** | **Sí** | No | No | Parcial |
| **Presupuesto de coste y estado de cuota** | **Sí** | No | No | Límite diario |
| **Neutralizar la configuración del usuario** | **Sí** | No | No | No |
| Flujos encadenados entre agentes | **Sí** | No | No | No |
| Ejecutar con el editor cerrado | Sí | **Sí** | **Sí** | Sí |
| Multiplataforma | No | **Sí** | **Sí** | Sí |
| Precio | 8–10 €/mes | **Gratis** | **Gratis** | **Gratis** |

¹ `/loop` reanuda dentro de la sesión, pero las opciones duraderas (escritorio, nube) arrancan en limpio.

Las seis primeras filas son el producto. Las tres últimas son donde perdemos.

**El worktree es la joya.** Todos los demás —Anthropic incluido— lanzan el agente sobre el directorio de trabajo del usuario. Si la tarea de las tres de la mañana se equivoca, se equivoca sobre su repositorio. Nosotros la encerramos en una copia y por la mañana usted decide. Es la única capacidad de la lista que no es una comodidad, sino una garantía.

---

## 5. Qué cambiar para superarles

### 5.1 Dejar de vender el calendario

La sección 2 de la ficha ya decía «el producto no se posicionará como un simple calendario». Ese instinto era correcto y ahora está demostrado. Hay que llevarlo hasta el final:

> **Antes:** «Programa prompts para tus agentes y que se ejecuten aunque VS Code esté cerrado.»
> **Ahora:** «Tus agentes trabajan de noche sin tocar tu repositorio. Por la mañana revisas el diff y decides.»

Programar es gratis en tres sitios. **Trabajar sin riesgo no lo ofrece nadie.**

### 5.2 Apoyarse en el programador del sistema, no competir con él

Adza demuestra que el Programador de tareas de Windows ya da gratis lo que nuestro demonio persistente reimplementa: durabilidad, supervivencia al reinicio y despertar del equipo. Nuestro runner sigue siendo necesario —los worktrees, los locks entre trabajos, el tope global, el canal de eventos en vivo y la cancelación no salen de una tarea suelta—, pero **el disparo debería venir del sistema operativo**, no de un bucle propio.

Ahorra parte de la Fase 1 y elimina de un plumazo el riesgo de «el demonio no estaba vivo».

### 5.3 Adelantar la revisión, retrasar el calendario

Si hay que recortar, el orden de la ficha está al revés. La Fase 3 —worktrees, permisos, diff— es **el producto**. La vista mensual de calendario es adorno. Sugiero mover la revisión de cambios a la Fase 2 y dejar el calendario en lista y semana.

### 5.4 Replantear el precio

Una suscripción de 8–10 € al mes frente a tres alternativas gratuitas es una conversación difícil de ganar con «te programa tareas». Es mucho más fácil de ganar con «te evita que un agente te rompa el repositorio de madrugada». Aun así, conviene revisar si el pago único de la familia `-Keeper` no encaja mejor aquí, dado que el diferenciador es una garantía y no un servicio continuo.

### 5.5 Copiar lo que hacen bien

- El botón **«Verify Setup»** de Adza, que comprueba el espacio de trabajo confiado antes de guardar la tarea. Nosotros encontramos el mismo problema; ellos ya tienen la solución de interfaz.
- Su disciplina de **no tocar nada que no hayan creado** en el programador del sistema.
- Los **placeholders de prompt** y el **historial con estado** de jjumman, que ya teníamos previstos.

### 5.6 Y la pregunta incómoda

Con Anthropic cubriendo la programación gratis y un competidor cubriendo lo mismo en tres plataformas, **merece la pena preguntarse si esto es un producto o una función de ChangeKeeper**.

ChangeKeeper ya está publicada y ya vive en el territorio de «protege y revisa los cambios del agente». El worktree aislado y la revisión de diff —nuestro diferenciador real— encajan ahí de forma natural. Programar sería entonces una función más, no un producto nuevo con su propio identificador, su propia licencia y sus propias nueve semanas.

No es una recomendación cerrada: TaskKeeper como producto propio tiene sentido si el multiproveedor y los flujos encadenados van en serio. Pero es la pregunta que yo haría en las 15 entrevistas, junto a la otra:

> **«¿Has intentado ya programar tareas de tu agente? ¿Qué usaste y qué pasó?»**

Si la respuesta mayoritaria es «no lo he intentado», el problema no es que falte producto: es que falta necesidad. Y eso se descubre en una semana de conversaciones, no en once.
