package adapters

import (
	"sort"
	"strings"
	"testing"
)

// Los dos agentes deben localizarse en esta máquina, donde NINGUNO está en el
// PATH: viven dentro de su extensión de VS Code.
func TestDeteccionFueraDelPath(t *testing.T) {
	for _, nombre := range []string{"claude", "codex"} {
		a, err := Para(nombre)
		if err != nil {
			t.Fatalf("Para(%s): %v", nombre, err)
		}
		inst, err := a.Detectar()
		if err != nil {
			t.Skipf("%s no está instalado en esta máquina: %v", nombre, err)
		}
		if inst.Ruta == "" || inst.Version == "" {
			t.Errorf("%s: detección incompleta %+v", nombre, inst)
		}
		t.Logf("%-7s v%-22s %s", nombre, inst.Version, inst.Ruta)
	}
}

// Con varias versiones instaladas hay que quedarse con la mayor, y por número,
// no por orden alfabético: 2.1.9 no es mayor que 2.1.10.
func TestOrdenPorVersionNoAlfabetico(t *testing.T) {
	dirs := []string{
		"C:/x/anthropic.claude-code-2.1.9-win32-x64",
		"C:/x/anthropic.claude-code-2.1.233-win32-x64",
		"C:/x/anthropic.claude-code-2.1.10-win32-x64",
	}
	sort.Sort(porVersion(dirs))
	if !strings.Contains(dirs[len(dirs)-1], "2.1.233") {
		t.Errorf("la mayor debería ser 2.1.233, quedó %q", dirs[len(dirs)-1])
	}
}

// El comando de Claude tiene que llevar siempre lo que la Fase 0 demostró
// obligatorio, y nunca un modo de permisos prohibido.
func TestComandoClaude(t *testing.T) {
	c := &Claude{ruta: "C:/falso/claude.exe", version: "2.1.234"}

	cmd, err := c.Comando(Peticion{
		Prompt: "revisa", Modo: ModoNuevo, NuevaSesionID: "11111111-2222-3333-4444-555555555555",
		DirTrabajo: "C:/proy", Perfil: PerfilAuditoria, MaxTurnos: 40,
		MaxPresupuesto: 2, FicheroSettings: "C:/perfil.json",
	})
	if err != nil {
		t.Fatalf("Comando: %v", err)
	}
	args := strings.Join(cmd.Args, " ")

	for _, obligatorio := range []string{
		"--output-format stream-json",
		"--verbose",                 // sin esto la ejecución falla
		"--permission-mode default", // no se confía en el valor por defecto
		"--settings C:/perfil.json", // neutraliza la configuración del usuario
		"--session-id 11111111",     // identificador fijado por nosotros
		"--max-turns 40",
		"--max-budget-usd 2.00",
	} {
		if !strings.Contains(args, obligatorio) {
			t.Errorf("falta %q en: %s", obligatorio, args)
		}
	}
	for _, prohibido := range []string{"bypassPermissions", "--dangerously-skip-permissions", "dontAsk"} {
		if strings.Contains(args, prohibido) {
			t.Errorf("aparece %q, que está prohibido", prohibido)
		}
	}

	// El perfil de auditoría no puede llevar herramientas de escritura.
	if !strings.Contains(args, "--disallowedTools Edit Write Bash") {
		t.Errorf("el perfil de auditoría no prohíbe la escritura: %s", args)
	}

	// Derivar usa el fork nativo.
	cmd, _ = c.Comando(Peticion{Prompt: "x", Modo: ModoDerivar, SesionExterna: "abc", Perfil: PerfilAuditoria})
	if !strings.Contains(strings.Join(cmd.Args, " "), "--resume abc --fork-session") {
		t.Errorf("el modo derivar no usa --fork-session")
	}
}

// Codex: sin `-a` (no existe en exec) y siempre con --ignore-user-config.
func TestComandoCodex(t *testing.T) {
	c := &Codex{ruta: "C:/falso/codex.exe", version: "0.148.0"}

	cmd, err := c.Comando(Peticion{
		Prompt: "audita", DirTrabajo: "C:/proy", Perfil: PerfilAuditoria,
	})
	if err != nil {
		t.Fatalf("Comando: %v", err)
	}
	args := strings.Join(cmd.Args, " ")

	if strings.Contains(args, " -a ") || strings.Contains(args, "--ask-for-approval") {
		t.Errorf("lleva -a, que NO existe en `codex exec`: %s", args)
	}
	for _, obligatorio := range []string{"exec", "--json", "-s read-only", "--ignore-user-config"} {
		if !strings.Contains(args, obligatorio) {
			t.Errorf("falta %q en: %s", obligatorio, args)
		}
	}
	if strings.Contains(args, "dangerously") || strings.Contains(args, "yolo") {
		t.Errorf("lleva un bypass de sandbox prohibido")
	}

	// El perfil aislado escribe, y solo dentro del worktree.
	cmd, _ = c.Comando(Peticion{Prompt: "x", Perfil: PerfilAislado,
		DirTrabajo: "C:/proy", Worktree: "C:/wt/rama"})
	args = strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "-s workspace-write") || !strings.Contains(args, "--cd C:/wt/rama") {
		t.Errorf("el perfil aislado no apunta al worktree: %s", args)
	}
}

// El fallo de autenticación llega como evento con campos, no como texto.
func TestParseoDeReintentoYResultado(t *testing.T) {
	c := &Claude{}

	ev, _ := c.Parsear([]byte(`{"type":"system","subtype":"api_retry","attempt":8,` +
		`"max_retries":10,"error_status":401,"error":"authentication_failed"}`))
	if ev.Tipo != EvReintentoAPI || ev.EstadoHTTP != 401 || ev.CodigoError != "authentication_failed" {
		t.Errorf("reintento mal interpretado: %+v", ev)
	}

	ev, _ = c.Parsear([]byte(`{"type":"system","subtype":"init","session_id":"s-1"}`))
	if ev.Tipo != EvSesionIniciada || ev.SesionID != "s-1" {
		t.Errorf("evento inicial mal interpretado: %+v", ev)
	}

	ev, _ = c.Parsear([]byte(`{"type":"result","subtype":"success","session_id":"s-1",` +
		`"total_cost_usd":0.42,"num_turns":7,"result":"listo"}`))
	if ev.Tipo != EvResultado || ev.Subtipo != "success" || ev.CosteUSD == nil || *ev.CosteUSD != 0.42 {
		t.Errorf("resultado mal interpretado: %+v", ev)
	}

	// Una línea que no es JSON se guarda como registro, nunca se interpreta.
	ev, _ = c.Parsear([]byte(`esto no es json`))
	if ev.Tipo != EvLog {
		t.Errorf("una línea suelta debería ser registro, fue %v", ev.Tipo)
	}
}

func TestCodexParseoHilo(t *testing.T) {
	c := &Codex{}
	ev, _ := c.Parsear([]byte(`{"type":"thread.started","thread_id":"0199a213-81c0"}`))
	if ev.Tipo != EvSesionIniciada || ev.SesionID != "0199a213-81c0" {
		t.Errorf("thread.started mal interpretado: %+v", ev)
	}
	ev, _ = c.Parsear([]byte(`{"type":"turn.started","thread_id":"t1","turn_id":"u1"}`))
	if ev.TurnoID != "u1" {
		t.Errorf("no guarda el turno, que hace falta para turn/interrupt: %+v", ev)
	}
}

// El entorno del hijo no debe arrastrar la sesión del padre. Medido en Fase 0.
func TestEntornoMinimoNoFiltraLaSesion(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "no-deberia-pasar")
	t.Setenv("ANTHROPIC_API_KEY", "secreto")

	env := entornoMinimo()
	for _, e := range env {
		k := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "ANTHROPIC") {
			t.Errorf("se filtró %q al proceso hijo", k)
		}
	}
	if len(env) == 0 {
		t.Errorf("el entorno mínimo quedó vacío: el proceso no arrancaría")
	}
}
