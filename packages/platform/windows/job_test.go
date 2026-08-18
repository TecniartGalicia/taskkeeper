//go:build windows

package windows

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// El propio binario de test hace de "intermedio": lanza un nieto de larga vida
// y termina sin esperarlo, dejándolo huérfano. Es exactamente el caso que
// `taskkill /T` no cubre, porque cuando se le pide matar el árbol el proceso
// intermedio ya no existe.
func TestMain(m *testing.M) {
	if os.Getenv("AC_ROLE") == "spawner" {
		spawnGrandchild()
		return
	}
	os.Exit(m.Run())
}

func spawnGrandchild() {
	// ping largo: proceso de larga vida que no necesita consola interactiva.
	c := exec.Command("ping", "-n", "600", "127.0.0.1")
	if err := c.Start(); err != nil {
		os.Stdout.WriteString("ERROR " + err.Error() + "\n")
		return
	}
	os.Stdout.WriteString(strconv.Itoa(c.Process.Pid) + "\n")
	// Sin Wait: el intermedio muere y el nieto queda huérfano.
}

// P-01. Criterio de aceptación 12: el timeout termina toda la jerarquía de
// procesos.
func TestJobObjectMataNietoHuerfano(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "AC_ROLE=spawner")
	job.Prepare(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := job.Adopt(cmd.Process.Pid); err != nil {
		job.Close()
		t.Fatalf("Adopt: %v", err)
	}

	line, _ := bufio.NewReader(stdout).ReadString('\n')
	nieto, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		job.Close()
		t.Fatalf("el intermedio no devolvió un PID: %q", strings.TrimSpace(line))
	}
	intermedio := cmd.Process.Pid

	// El intermedio termina; el nieto queda huérfano y reasignado.
	if err := cmd.Wait(); err != nil {
		t.Logf("el intermedio salió con %v (esperable)", err)
	}
	if Alive(intermedio) {
		t.Errorf("el intermedio %d debería haber terminado", intermedio)
	}
	if !waitFor(func() bool { return Alive(nieto) }, 2*time.Second) {
		job.Close()
		t.Fatalf("el nieto %d no llegó a arrancar; la prueba no demuestra nada", nieto)
	}
	t.Logf("nieto %d vivo y huérfano (intermedio %d muerto)", nieto, intermedio)

	// Cerrar el job debe bastar: KILL_ON_JOB_CLOSE.
	if err := job.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !waitFor(func() bool { return !Alive(nieto) }, 5*time.Second) {
		// Limpieza para no dejar el ping colgado si la prueba falla.
		exec.Command("taskkill", "/PID", strconv.Itoa(nieto), "/F").Run()
		t.Fatalf("el nieto %d sobrevivió al cierre del job: KILL_ON_JOB_CLOSE no cumple", nieto)
	}
	t.Logf("nieto %d muerto tras cerrar el job", nieto)
}

// KillAll explícito, que es la ruta de FR-024 al cancelar una ejecución.
func TestJobObjectKillAllExplicito(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	defer job.Close()

	cmd := exec.Command("ping", "-n", "600", "127.0.0.1")
	job.Prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := job.Adopt(cmd.Process.Pid); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	pid := cmd.Process.Pid

	if err := job.KillAll(); err != nil {
		t.Fatalf("KillAll: %v", err)
	}
	if !waitFor(func() bool { return !Alive(pid) }, 5*time.Second) {
		t.Fatalf("el proceso %d sobrevivió a KillAll", pid)
	}
	_ = cmd.Wait()
}

func waitFor(cond func() bool, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}
