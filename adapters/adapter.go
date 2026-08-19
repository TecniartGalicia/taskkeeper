// Package adapters traduce entre TaskKeeper y cada agente.
//
// Regla de diseño (D9 del plan): los adaptadores CONSTRUYEN el comando, no lo
// lanzan. El worker es quien lo arranca, lo mete en su Job Object, lo vigila y
// lo mata. Así el control de procesos vive en un solo sitio.
package adapters

import (
	"errors"
	"os/exec"
	"time"
)

type Modo string

const (
	ModoNuevo   Modo = "new"
	ModoRetomar Modo = "resume"
	ModoDerivar Modo = "fork"
)

type Perfil string

const (
	PerfilAuditoria Perfil = "auditoria"
	PerfilAislado   Perfil = "cambios_aislados"
)

type Peticion struct {
	Prompt          string
	Modo            Modo
	SesionExterna   string // para retomar o derivar
	NuevaSesionID   string // UUID fijado por nosotros (FR-008)
	DirTrabajo      string
	Worktree        string
	Perfil          Perfil
	MaxTurnos       int
	MaxPresupuesto  float64
	FicheroSettings string // perfil de permisos controlado, para Claude
}

type TipoEvento string

const (
	EvSesionIniciada TipoEvento = "sesion_iniciada"
	EvMensaje        TipoEvento = "mensaje"
	EvResultado      TipoEvento = "resultado"
	EvReintentoAPI   TipoEvento = "reintento_api"
	EvLog            TipoEvento = "log"
)

type Evento struct {
	Tipo        TipoEvento
	SesionID    string
	TurnoID     string
	Subtipo     string // success | error_max_turns | error_max_budget_usd
	EstadoHTTP  int    // en reintentos: 401, 429...
	CodigoError string
	CosteUSD    *float64
	Turnos      *int
	Texto       string
	Bruto       string
	// Cuándo dice el proveedor que vuelve la cuota, si lo dice. Cero si no.
	ReinicioCuota time.Time
}

type Capacidades struct {
	Historial       bool
	Retomar         bool
	Derivar         bool
	SalidaEstruct   bool
	MaxTurnos       bool
	Presupuesto     bool
	Sandbox         bool
	SesionPrefijada bool
}

type Instalacion struct {
	Ruta    string
	Version string
}

type Adaptador interface {
	Nombre() string
	Detectar() (*Instalacion, error)
	Capacidades() Capacidades
	Comando(p Peticion) (*exec.Cmd, error)
	Parsear(linea []byte) (Evento, bool)
}

var (
	ErrNoInstalado     = errors.New("agente no encontrado")
	ErrPerfilProhibido = errors.New("perfil de permisos no admitido")
)

// Desenlace es lo que el worker guarda al terminar.
type Desenlace struct {
	Estado      string
	Codigo      string
	CosteUSD    *float64
	Subtipo     string
	ReintentarA *time.Time
	Detalle     string
}

func Para(nombre string) (Adaptador, error) {
	switch nombre {
	case "claude":
		return &Claude{}, nil
	case "codex":
		return &Codex{}, nil
	}
	return nil, errors.New("agente desconocido: " + nombre)
}

// entornoMinimo recorta lo que hereda el proceso hijo. Medido en la Fase 0: el
// entorno de una sesión de Claude Code se filtra al hijo y le cambia el
// comportamiento.
func entornoMinimo(extra ...string) []string {
	conservar := []string{"SystemRoot", "windir", "TEMP", "TMP", "PATH", "Path",
		"USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA", "PATHEXT", "COMSPEC",
		"NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "HOMEDRIVE", "HOMEPATH"}
	var env []string
	for _, k := range conservar {
		if v, ok := lookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env, extra...)
}
