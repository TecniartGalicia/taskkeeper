// Package config resuelve dónde vive todo. Una sola definición para el worker,
// la herramienta de gestión y la extensión.
package config

import (
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Raiz         string
	DB           string
	DirTurnos    string
	DirWorktrees string
	DirPerfiles  string
	Worker       string
}

func Cargar() Config { return CargarEn("") }

// CargarEn permite fijar la raíz desde fuera. Lo necesita el disparador del
// sistema, que no hereda el entorno de quien creó la tarea.
func CargarEn(raiz string) Config {
	if raiz == "" {
		raiz = os.Getenv("TASKKEEPER_HOME")
	}
	if raiz == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base, _ = os.UserHomeDir()
		}
		raiz = filepath.Join(base, "Argalla", "TaskKeeper")
	}
	exe, _ := os.Executable()
	return Config{
		Raiz:         raiz,
		DB:           filepath.Join(raiz, "taskkeeper.db"),
		DirTurnos:    filepath.Join(raiz, "turnos"),
		DirWorktrees: filepath.Join(raiz, "worktrees"),
		DirPerfiles:  filepath.Join(raiz, "perfiles"),
		Worker:       filepath.Join(filepath.Dir(exe), "taskkeeper-worker"+exeSufijo()),
	}
}

func (c Config) Preparar() error {
	for _, d := range []string{c.Raiz, c.DirTurnos, c.DirWorktrees, c.DirPerfiles} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// RutaMarca es el fichero que la extensión observa. Tocarlo es la única señal
// que viaja del worker a la interfaz: sin canal, sin sondeo de la base.
func RutaMarca(c Config) string { return filepath.Join(c.Raiz, "cambios.marca") }

func TocarMarca(c Config) {
	p := RutaMarca(c)
	now := time.Now()
	if err := os.Chtimes(p, now, now); err != nil {
		os.WriteFile(p, []byte(now.UTC().Format(time.RFC3339Nano)), 0o644)
		return
	}
	// Chtimes basta para que fs.watch avise, pero algunos observadores solo
	// reaccionan al contenido; se escribe además el instante.
	os.WriteFile(p, []byte(now.UTC().Format(time.RFC3339Nano)), 0o644)
}
