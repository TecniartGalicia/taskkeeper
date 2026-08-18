package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	Error    struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error"`
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
	case "error":
		return Evento{Tipo: EvReintentoAPI, EstadoHTTP: l.Error.Status,
			CodigoError: l.Error.Message}, true
	}
	return Evento{Tipo: EvMensaje, Bruto: string(linea)}, true
}
