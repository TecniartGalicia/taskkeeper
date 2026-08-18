# Condiciones de uso — informe previo a la Fase 1

Agent Calendar · Argalla · 18 de agosto de 2026

**Pregunta:** ¿es admisible que un producto de terceros ejecute tareas programadas y desatendidas contra Claude Code y Codex usando la cuenta de suscripción del propio usuario?

**Respuesta corta: sí, con una salvedad menor que no bloquea la Fase 1.**

---

## Qué he podido verificar y qué no

| Fuente | Estado |
|---|---|
| Política de uso de Anthropic | ✅ Leída |
| Comportamiento del producto de Anthropic | ✅ Verificado en la máquina |
| Comportamiento del producto de OpenAI | ✅ Verificado en la máquina |
| Términos de servicio de OpenAI | ⚠️ **No leídos**: el sitio devuelve 403 a la lectura automatizada |

La salvedad está en la última fila y se resuelve con una lectura humana de diez minutos.

## Anthropic

La política de uso **no prohíbe** el uso programático, desatendido ni por herramientas de terceros. Lo que sí prohíbe, y no nos afecta:

- Usar automatización **para crear cuentas** o para conducta de tipo spam.
- Eludir un bloqueo mediante otra cuenta.
- Saltarse las barreras del modelo a propósito.
- Usar entradas y salidas para **entrenar otro modelo**.

Ninguna describe lo que hace Agent Calendar: el usuario aporta su propia cuenta, ya creada y ya autenticada, y el producto se limita a lanzar el mismo binario que él lanzaría a mano.

Y hay algo más fuerte que la ausencia de prohibición: **Anthropic distribuye las piezas para hacer exactamente esto**. Verificado en la máquina de pruebas:

- Modo no interactivo `claude -p` con salida estructurada.
- Un SDK de agentes con API pública de sesiones, que es la que usa este producto.
- Tareas programadas y rutinas como funciones del propio Claude Code.

Automatizar Claude Code no es una zona gris: es una capacidad que el fabricante documenta y publica.

## OpenAI

No he podido leer los términos: `openai.com/policies` responde **403** a la lectura automatizada. Queda como la única tarea de lectura humana pendiente.

Lo que sí he verificado, ejecutando el binario:

- **`codex exec`** existe y su propia ayuda lo describe para *«trabajo con guiones o de tipo integración continua»*. Es decir, el uso no interactivo es su motivo de existir.
- **`codex app-server`** expone un protocolo JSON-RPC y el binario **publica el esquema JSON completo** de ese protocolo mediante `generate-json-schema`. Publicar el esquema de un protocolo es lo que se hace cuando se espera que **clientes de terceros** lo usen.
- **`codex mcp-server`** permite integrarlo en otras herramientas.

Un fabricante que publica el esquema de su protocolo para que otros escriban clientes está invitando a escribirlos. La probabilidad de que sus términos prohíban justo eso es baja, pero *baja* no es *cero*, y por eso queda la lectura pendiente.

## Lo que sí sería un problema, y no hacemos

| Práctica prohibida en cualquiera de las dos | ¿Agent Calendar la hace? |
|---|---|
| Revender acceso al modelo | **No.** Se cobra por la orquestación; el consumo lo paga el usuario con su cuenta |
| Compartir credenciales entre usuarios | **No.** Cada instalación usa la credencial local de su dueño; el producto no las almacena ni las transmite |
| Crear cuentas automáticamente | **No** |
| Eludir límites de uso | **No.** Al contrario: el producto añade tope de gasto por ejecución y presupuesto diario |
| Enviar código o conversaciones a Argalla | **No.** El MVP es local |

Ese último punto es el que más protege comercialmente: al no tocar nunca las credenciales ni el contenido, Agent Calendar tiene la misma relación con el proveedor que un lanzador de tareas del sistema operativo.

## Los riesgos reales no son legales

Son operativos, y ya están recogidos en el plan:

1. **Límites de uso de las suscripciones.** Una tarea nocturna puede agotar la cuota del día y dejar al usuario sin agente por la mañana. Mitigado con `--max-budget-usd` por ejecución, presupuesto diario y el estado `failed_quota`.
2. **Protocolo inestable.** `codex app-server` está marcado como experimental y la versión instalada es una alfa. Mitigado con la degradación a `codex exec` y con regenerar el esquema en cada actualización.
3. **Percepción del proveedor.** Aunque sea admisible, conviene que el producto no parezca un intento de exprimir una suscripción: los valores por defecto son conservadores (una ejecución simultánea, presupuesto acotado, solo lectura) y así deben quedarse.

## Recomendación

**Abrir la puerta y empezar la Fase 1.** La única acción pendiente es que usted lea una vez los términos de servicio de OpenAI, que yo no puedo descargar. Si apareciese una cláusula que exija credenciales de API de pago por uso en lugar de suscripción, la salida ya está diseñada: el adaptador acepta ambas, y sería un cambio de configuración, no de arquitectura.
