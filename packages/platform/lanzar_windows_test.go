//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// "Ejecutar ahora" no puede bloquear la extensión. Antes esperaba a que el
// agente terminase, así que el botón dejaba la interfaz colgada media hora.
func TestLanzarWorkerNoBloquea(t *testing.T) {
	// Un "worker" que tarda treinta segundos.
	bat := filepath.Join(t.TempDir(), "lento.bat")
	if err := os.WriteFile(bat, []byte("@echo off\r\nping -n 30 127.0.0.1 >nul\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	inicio := time.Now()
	if err := LanzarWorker(bat, "tarea-de-prueba", time.Now().UTC()); err != nil {
		t.Fatalf("LanzarWorker: %v", err)
	}
	tardo := time.Since(inicio)

	if tardo > 3*time.Second {
		t.Fatalf("tardó %s: sigue esperando a que el worker termine", tardo.Round(time.Millisecond))
	}
	t.Logf("devolvió el control en %s con el worker aún trabajando", tardo.Round(time.Millisecond))
}
