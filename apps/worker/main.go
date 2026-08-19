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
		cupo       = flag.Int("cupo", 0, "sobrescribe el cupo de simultáneas; 0 = usar el ajuste guardado")
		manual     = flag.Bool("manual", false, "arranque a mano: no deriva la hora prevista")
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
	derivar := !*manual
	if *ocurrencia != "" {
		derivar = false
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
	// Cada cambio de estado toca la marca que observa la extensión.
	db.OnChange = func() { config.TocarMarca(cfg) }

	// Ctrl+C o cierre de sesión: se propaga como cancelación ordenada. El grupo
	// de procesos se cierra igualmente por el defer del runner.
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer parar()

	o := runner.PorDefecto()
	// El cupo es un ajuste de la máquina y vive en la base. Antes iba en la línea
	// de órdenes de cada disparador, así que cambiarlo obligaba a reescribirlos
	// todos.
	o.CupoSimultaneo = db.Cupo()
	if *cupo > 0 {
		o.CupoSimultaneo = *cupo
	}
	o.DirTurnos = cfg.DirTurnos
	o.DirWorktrees = cfg.DirWorktrees

	// Sin --occurrence ni --manual, la hora prevista se deriva de la regla: el
	// disparador del sistema no nos dice de qué hora venimos.
	ejecutar := runner.Ejecutar
	if derivar {
		ejecutar = runner.EjecutarProgramada
	}
	if err := ejecutar(ctx, db, runner.DepsReales(), o, *taskID, cuando); err != nil {
		fmt.Fprintln(os.Stderr, "ejecutando:", err)
		os.Exit(1)
	}
}
