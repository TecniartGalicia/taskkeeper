package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Claude struct {
	ruta    string
	version string
}

func (c *Claude) Nombre() string { return "claude" }

// Detectar localiza el binario. Hallazgo de la Fase 0: Claude Code NO está en el
// PATH en una instalación normal de VS Code. Vive dentro de la carpeta de su
// extensión, con la versión en la ruta, y conviven varias versiones. Por eso se
// busca por patrón y se ordena por versión, y por eso se repite en cada
// preflight: la ruta caduca en cuanto VS Code actualiza.
func (c *Claude) Detectar() (*Instalacion, error) {
	if r, err := exec.LookPath("claude"); err == nil {
		return c.version_de(r)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	patron := filepath.Join(home, ".vscode", "extensions", "anthropic.claude-code-*")
	dirs, _ := filepath.Glob(patron)
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%w: claude", ErrNoInstalado)
	}
	sort.Sort(porVersion(dirs))
	for i := len(dirs) - 1; i >= 0; i-- {
		cand := filepath.Join(dirs[i], "resources", "native-binary", "claude"+exeSufijo())
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return c.version_de(cand)
		}
	}
	return nil, fmt.Errorf("%w: claude", ErrNoInstalado)
}

func (c *Claude) version_de(ruta string) (*Instalacion, error) {
	out, err := exec.Command(ruta, "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("%q no responde a --version: %w", ruta, err)
	}
	c.ruta = ruta
	c.version = strings.TrimSpace(strings.Fields(string(out))[0])
	return &Instalacion{Ruta: ruta, Version: c.version}, nil
}

func (c *Claude) Capacidades() Capacidades {
	return Capacidades{
		Historial: true, Retomar: true, Derivar: true, SalidaEstruct: true,
		MaxTurnos: true, Presupuesto: true, SesionPrefijada: true,
		Sandbox: false, // el aislamiento lo da el worktree, no el CLI
	}
}

// Modos de permiso prohibidos. La lista es por MODO y no por la opción
// --dangerously-skip-permissions, que es solo un alias de bypassPermissions:
// bloquear el alias dejaría la puerta abierta.
var modosProhibidos = map[string]bool{
	"bypassPermissions": true,
	"auto":              true,
	"dontAsk":           true,
}

func (c *Claude) Comando(p Peticion) (*exec.Cmd, error) {
	if c.ruta == "" {
		if _, err := c.Detectar(); err != nil {
			return nil, err
		}
	}

	args := []string{
		"-p", p.Prompt,
		"--output-format", "stream-json",
		// OBLIGATORIO con stream-json en modo -p. Sin --verbose la ejecución
		// falla, y es el error que más tiempo cuesta diagnosticar.
		"--verbose",
	}

	switch p.Modo {
	case ModoNuevo:
		if p.NuevaSesionID != "" {
			// Fijamos el identificador desde aquí: si el proceso muere antes de
			// emitir el evento inicial, la ejecución sigue atada a su sesión.
			args = append(args, "--session-id", p.NuevaSesionID)
		}
	case ModoRetomar:
		args = append(args, "--resume", p.SesionExterna)
	case ModoDerivar:
		args = append(args, "--resume", p.SesionExterna, "--fork-session")
	}

	modo, permitidas, prohibidas := permisosClaude(p.Perfil)
	if modosProhibidos[modo] {
		return nil, fmt.Errorf("%w: %s", ErrPerfilProhibido, modo)
	}
	args = append(args, "--permission-mode", modo)

	// Medido en la Fase 0: sin --settings, la configuración del usuario puede
	// conceder mucho más de lo que el perfil pretende. Con ella arrancaba en
	// bypassPermissions y con dieciséis permisos ya autorizados.
	if p.FicheroSettings != "" {
		args = append(args, "--settings", p.FicheroSettings)
	}
	if len(permitidas) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, permitidas...)
	}
	if len(prohibidas) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, prohibidas...)
	}
	if p.MaxTurnos > 0 {
		args = append(args, "--max-turns", strconv.Itoa(p.MaxTurnos))
	}
	if p.MaxPresupuesto > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(p.MaxPresupuesto, 'f', 2, 64))
	}
	if p.Worktree != "" {
		args = append(args, "--add-dir", p.Worktree)
	}

	// exec.Command escapa cada argumento por separado: nunca se construye una
	// cadena de shell.
	cmd := exec.Command(c.ruta, args...)
	cmd.Dir = p.DirTrabajo
	cmd.Env = entornoMinimo()
	return cmd, nil
}

func permisosClaude(perfil Perfil) (modo string, permitidas, prohibidas []string) {
	switch perfil {
	case PerfilAislado:
		return "acceptEdits",
			[]string{"Read", "Glob", "Grep", "Edit", "Write"},
			[]string{"Bash(git push*)", "Bash(git merge*)", "Bash(gh *)"}
	default: // auditoría
		return "default",
			[]string{"Read", "Glob", "Grep"},
			[]string{"Edit", "Write", "Bash"}
	}
}

type lineaClaude struct {
	Type         string   `json:"type"`
	Subtype      string   `json:"subtype"`
	SessionID    string   `json:"session_id"`
	TotalCostUSD *float64 `json:"total_cost_usd"`
	NumTurns     *int     `json:"num_turns"`
	Result       string   `json:"result"`
	ErrorStatus  int      `json:"error_status"`
	Error        string   `json:"error"`
}

func (c *Claude) Parsear(linea []byte) (Evento, bool) {
	var l lineaClaude
	if err := json.Unmarshal(linea, &l); err != nil {
		// Lo que no es JSON se guarda como registro, nunca se interpreta como
		// estado: adivinar aquí es cómo se clasifica mal un fallo.
		return Evento{Tipo: EvLog, Texto: string(linea)}, true
	}
	switch {
	case l.Type == "system" && l.Subtype == "init":
		return Evento{Tipo: EvSesionIniciada, SesionID: l.SessionID}, true
	case l.Type == "system" && l.Subtype == "api_retry":
		// El hallazgo de la Fase 0: el fallo llega como evento con campos, no
		// como texto traducible. Y el CLI reintenta diez veces con espera
		// creciente, así que el worker corta al primero.
		return Evento{Tipo: EvReintentoAPI, EstadoHTTP: l.ErrorStatus,
			CodigoError: l.Error, SesionID: l.SessionID}, true
	case l.Type == "result":
		return Evento{Tipo: EvResultado, SesionID: l.SessionID, Subtipo: l.Subtype,
			CosteUSD: l.TotalCostUSD, Turnos: l.NumTurns, Texto: l.Result}, true
	}
	return Evento{Tipo: EvMensaje, Bruto: string(linea)}, true
}
