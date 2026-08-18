// taskkeeper-worker ejecuta una ocurrencia y termina.
//
// Lo lanza el programador del sistema operativo a la hora prevista, o la
// extensión cuando alguien pulsa "Ejecutar ahora". No queda residente: si algo
// tiene que estar vivo esperando, es el sistema, no nosotros.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/argalla/taskkeeper/packages/config"
	"github.com/argalla/taskkeeper/packages/runner"
	"github.com/argalla/taskkeeper/packages/store"
)

var version = "dev"

func main() {
	var (
		taskID     = flag.String("run", "", "identificador de la tarea a ejecutar")
		ocurrencia = flag.String("occurrence", "", "instante previsto en RFC3339 (por omisión, ahora)")
		cupo       = flag.Int("cupo", 1, "ejecuciones simultáneas permitidas en la máquina")
		home       = flag.String("home", "", "raíz de datos; el disparador del sistema no hereda el entorno")
		verVersion = flag.Bool("version", false, "muestra la versión y sale")
	)
	flag.Parse()

	if *verVersion {
		fmt.Println("taskkeeper-worker", version)
		return
	}
	if *taskID == "" {
		fmt.Fprintln(os.Stderr, "falta --run <id de tarea>")
		os.Exit(2)
	}

	cuando := time.Now().UTC()
	if *ocurrencia != "" {
		t, err := time.Parse(time.RFC3339, *ocurrencia)
		if err != nil {
			fmt.Fprintf(os.Stderr, "instante inválido %q: %v\n", *ocurrencia, err)
			os.Exit(2)
		}
		cuando = t.UTC()
	}

	cfg := config.CargarEn(*home)
	if err := cfg.Preparar(); err != nil {
		fmt.Fprintln(os.Stderr, "preparando el directorio de datos:", err)
		os.Exit(1)
	}
	db, err := store.Open(cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "abriendo la base:", err)
		os.Exit(1)
	}
	defer db.Close()

	// Ctrl+C o cierre de sesión: se propaga como cancelación ordenada. El grupo
	// de procesos se cierra igualmente por el defer del runner.
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer parar()

	o := runner.PorDefecto()
	o.CupoSimultaneo = *cupo
	o.DirTurnos = cfg.DirTurnos
	o.DirWorktrees = cfg.DirWorktrees

	if err := runner.Ejecutar(ctx, db, runner.DepsReales(), o, *taskID, cuando); err != nil {
		fmt.Fprintln(os.Stderr, "ejecutando:", err)
		os.Exit(1)
	}
}
