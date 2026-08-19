# Privacidad

TaskKeeper funciona en local. No tiene cuenta, ni telemetría, ni ningún servicio de red propio.

## Qué se queda en tu equipo

- **Tus prompts, la salida de los agentes, los diffs, el coste y el historial** viven en una base SQLite local en tu perfil de usuario (`%LOCALAPPDATA%\Argalla\TaskKeeper` en Windows). TaskKeeper no sube nada de eso.
- **Las tareas programadas** son entradas del planificador de tu propio sistema operativo, bajo una carpeta/prefijo exclusivos de TaskKeeper. TaskKeeper nunca lee ni cambia tareas que no haya creado él.
- **Los secretos** que un agente pueda llegar a imprimir (claves de API, tokens, claves privadas) se redactan antes de escribirse en la base, en un único punto.

## Qué sí sale de tu equipo — y de quién es la responsabilidad

TaskKeeper lanza **Claude Code** (Anthropic) y/o **Codex** (OpenAI) con la instalación y las credenciales que **tú** ya tienes. Al ejecutarse una tarea, esos agentes envían tu prompt y el contexto del repositorio a sus proveedores, igual que cuando los ejecutas a mano. Ese tráfico se rige por **sus** políticas de privacidad, no por esta:

- Anthropic — https://www.anthropic.com/legal/privacy
- OpenAI — https://openai.com/policies/privacy-policy

TaskKeeper no añade telemetría por encima y no envía nada a Argalla ni a nadie más.

## Contacto

Tecniart Galicia SL (Argalla) — info@tecniartgalicia.com
