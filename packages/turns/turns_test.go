package turns

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// P-23, primera mitad: con un solo turno, el segundo aspirante no entra.
func TestUnTurnoSerializa(t *testing.T) {
	dir := t.TempDir()

	s1, err := Acquire(1, dir, 0)
	if err != nil {
		t.Fatalf("el primero no consiguió turno: %v", err)
	}

	if _, err := Acquire(1, dir, 0); err == nil {
		t.Fatalf("el segundo consiguió turno con cupo 1: el cupo no se respeta")
	} else if err != ErrSinTurno {
		t.Fatalf("error inesperado: %v", err)
	}

	s1.Release()

	s2, err := Acquire(1, dir, 0)
	if err != nil {
		t.Fatalf("tras liberar, el turno debería estar disponible: %v", err)
	}
	s2.Release()
}

func TestCupoMayorQueUno(t *testing.T) {
	dir := t.TempDir()
	var held []*Slot
	for i := 0; i < 3; i++ {
		s, err := Acquire(3, dir, 0)
		if err != nil {
			t.Fatalf("turno %d denegado con cupo 3: %v", i+1, err)
		}
		held = append(held, s)
	}
	if _, err := Acquire(3, dir, 0); err != ErrSinTurno {
		t.Fatalf("el cuarto entró con cupo 3")
	}
	for _, s := range held {
		s.Release()
	}
}

// P-23, segunda mitad y la que de verdad importa: un worker que muere de golpe
// NO deja el turno bloqueado para siempre. Es el fallo clásico de repartir cupo
// con ficheros marcador, y la razón de apoyarse en el bloqueo del sistema.
func TestProcesoMuertoLiberaSuTurno(t *testing.T) {
	dir := t.TempDir()

	// Un hijo toma el turno y se queda esperando; lo matamos sin miramientos.
	cmd := exec.Command(os.Args[0], "-test.run=TestAyudanteTomaTurno")
	cmd.Env = append(os.Environ(), "TK_TURNOS_DIR="+dir, "TK_ROL=ayudante")
	salida, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 64)
	n, _ := salida.Read(buf)
	if !strings.Contains(string(buf[:n]), "TOMADO") {
		cmd.Process.Kill()
		t.Fatalf("el ayudante no llegó a tomar el turno: %q", string(buf[:n]))
	}

	// Con el hijo vivo, no debe haber turno.
	if s, err := Acquire(1, dir, 0); err == nil {
		s.Release()
		cmd.Process.Kill()
		t.Fatalf("había turno libre mientras el ayudante lo tenía")
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("no se pudo matar al ayudante: %v", err)
	}
	cmd.Wait()

	// El sistema debe haber soltado el bloqueo al cerrar el proceso.
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, err := Acquire(1, dir, 0)
		if err == nil {
			s.Release()
			return // correcto
		}
		if time.Now().After(deadline) {
			contenido, _ := os.ReadFile(filepath.Join(dir, "turno-0.lock"))
			t.Fatalf("el turno siguió bloqueado tras morir el proceso; contenido: %q", contenido)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Ayudante: solo se ejecuta cuando lo invoca la prueba anterior.
func TestAyudanteTomaTurno(t *testing.T) {
	if os.Getenv("TK_ROL") != "ayudante" {
		t.Skip("no es el ayudante")
	}
	s, err := Acquire(1, os.Getenv("TK_TURNOS_DIR"), 0)
	if err != nil {
		os.Stdout.WriteString("FALLO\n")
		return
	}
	_ = s
	os.Stdout.WriteString("TOMADO\n")
	os.Stdout.Sync()
	time.Sleep(60 * time.Second) // esperamos a que nos maten
}
