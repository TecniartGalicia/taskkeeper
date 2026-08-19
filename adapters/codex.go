package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Codex struct {
	ruta    string
	version string
}

func (c *Codex) Nombre() string { return "codex" }

// Detectar: igual que Claude, Codex vive dentro de su extensión de VS Code y no
// en el PATH. La ruta lleva la versión y caduca al actualizar.
func (c *Codex) Detectar() (*Instalacion, error) {
	if r, err := exec.LookPath("codex"); err == nil {
		return c.version_de(r)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dirs, _ := filepath.Glob(filepath.Join(home, ".vscode", "extensions", "openai.chatgpt-*"))
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%w: codex", ErrNoInstalado)
	}
	sort.Sort(porVersion(dirs))
	for i := len(dirs) - 1; i >= 0; i-- {
		for _, sub := range []string{
			filepath.Join("bin", "windows-x86_64", "codex"+exeSufijo()),
			filepath.Join("bin", "codex"+exeSufijo()),
		} {
			cand := filepath.Join(dirs[i], sub)
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return c.version_de(cand)
			}
		}
	}
	return nil, fmt.Errorf("%w: codex", ErrNoInstalado)
}

func (c *Codex) version_de(ruta string) (*Instalacion, error) {
	out, err := exec.Command(ruta, "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("%q no responde a --version: %w", ruta, err)
	}
	c.ruta = ruta
	campos := strings.Fields(string(out))
	c.version = campos[len(campos)-1]
	return &Instalacion{Ruta: ruta, Version: c.version}, nil
}

func (c *Codex) Capacidades() Capacidades {
	return Capacidades{
		Historial: true, Retomar: true, SalidaEstruct: true, Sandbox: true,
		// thread/fork existe y está verificado a nivel de esquema, pero solo por
		// App Server. Por la vía `codex exec` de la Fase 1, no.
		Derivar: false,
	}
}

func (c *Codex) Comando(p Peticion) (*exec.Cmd, error) {
	if c.ruta == "" {
		if _, err := c.Detectar(); err != nil {
			return nil, err
		}
	}

	args := []string{"exec"}
	if p.Modo == ModoRetomar && p.SesionExterna != "" {
		args = append(args, "resume", p.SesionExterna)
	}

	sandbox := "read-only"
	dir := p.DirTrabajo
	if p.Perfil == PerfilAislado {
		sandbox = "workspace-write"
		if p.Worktree != "" {
			dir = p.Worktree
		}
	}

	args = append(args,
		"--json",
		"--cd", dir,
		"-s", sandbox,
		// Obligatoria en toda ejecución programada: sin ella el config.toml del
		// usuario puede conceder más de lo que el perfil pretende. Es el
		// equivalente de --settings en Claude Code.
		"--ignore-user-config",
	)
	// Nota de la Fase 0: `-a` / `--ask-for-approval` NO existe en `codex exec`
	// (devuelve "unexpected argument"). Es del modo interactivo. exec no
	// pregunta porque no es interactivo; el aislamiento lo fija -s y solo -s.

	args = append(args, p.Prompt)

	cmd := exec.Command(c.ruta, args...)
	cmd.Env = entornoMinimo()
	return cmd, nil
}

type lineaCodex struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	// Un evento {"type":"error"} lleva el mensaje en la raíz; turn.failed lo
	// lleva anidado. Se aceptan las dos formas.
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error"`
}

// Verificado con el agente real: el error de cuota de Codex NO trae código
// HTTP, solo texto:
//
//	{"type":"error","message":"You've hit your usage limit. ... try again at Aug 20th, 2026 6:39 PM."}
//
// Así que aquí sí hace falta la tabla de patrones que en Claude sobra. Ante la
// duda se deja EstadoHTTP en 0 y el runner lo tratará como fallo genérico, que
// es el lado seguro.
var (
	reCuotaCodex = regexp.MustCompile(`(?i)usage limit|rate limit|quota|too many requests|try again at`)
	reAuthCodex  = regexp.MustCompile(`(?i)not logged in|unauthori[sz]ed|login required|invalid api key|authentication`)
)

func clasificarMensajeCodex(msg string) int {
	switch {
	case reAuthCodex.MatchString(msg):
		return 401
	case reCuotaCodex.MatchString(msg):
		return 429
	}
	return 0
}

func (c *Codex) Parsear(linea []byte) (Evento, bool) {
	var l lineaCodex
	if err := json.Unmarshal(linea, &l); err != nil {
		return Evento{Tipo: EvLog, Texto: string(linea)}, true
	}
	switch l.Type {
	case "thread.started":
		return Evento{Tipo: EvSesionIniciada, SesionID: l.ThreadID}, true
	case "turn.started":
		// Hace falta guardarlo: turn/interrupt exige threadId Y turnId.
		return Evento{Tipo: EvMensaje, SesionID: l.ThreadID, TurnoID: l.TurnID}, true
	case "turn.completed":
		return Evento{Tipo: EvResultado, SesionID: l.ThreadID, Subtipo: "success"}, true
	case "error", "turn.failed":
		msg := l.Message
		if msg == "" {
			msg = l.Error.Message
		}
		estado := l.Error.Status
		if estado == 0 {
			estado = clasificarMensajeCodex(msg)
		}
		return Evento{Tipo: EvReintentoAPI, EstadoHTTP: estado, CodigoError: recortar(msg, 160),
			ReinicioCuota: horaDeReinicioCodex(msg)}, true
	}
	return Evento{Tipo: EvMensaje, Bruto: string(linea)}, true
}

// "try again at Aug 20th, 2026 6:39 PM" → hora local. Si no se entiende, cero:
// el runner aplicará su espera por defecto.
var reReinicio = regexp.MustCompile(`try again at ([A-Za-z]{3,9}) (\d{1,2})(?:st|nd|rd|th)?,? (\d{4}) (\d{1,2}:\d{2} ?[AP]M)`)

func horaDeReinicioCodex(msg string) time.Time {
	m := reReinicio.FindStringSubmatch(msg)
	if m == nil {
		return time.Time{}
	}
	cadena := fmt.Sprintf("%s %s %s %s", m[1], m[2], m[3], strings.ReplaceAll(m[4], " ", ""))
	for _, layout := range []string{"Jan 2 2006 3:04PM", "January 2 2006 3:04PM"} {
		if t, err := time.ParseInLocation(layout, cadena, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func recortar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
