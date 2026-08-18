// Package turns reparte el cupo de ejecuciones simultáneas entre procesos que
// no se conocen entre sí.
//
// Sin demonio no hay coordinador, así que el cupo se materializa como N ficheros:
// quien consigue abrir uno en exclusiva tiene turno. La propiedad que hace que
// esto funcione y que a la técnica clásica del "fichero marcador" le falta: el
// bloqueo lo mantiene el sistema operativo sobre un descriptor abierto, así que
// un worker que muere de golpe lo libera solo. No quedan turnos huérfanos.
package turns

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrSinTurno = errors.New("no hay turno libre")

type Slot struct {
	f    *os.File
	path string
}

func (s *Slot) Path() string { return s.path }

func (s *Slot) Release() {
	if s == nil || s.f == nil {
		return
	}
	unlock(s.f)
	s.f.Close()
	s.f = nil
}

// Acquire intenta hacerse con uno de los n turnos, reintentando hasta agotar
// wait. Con wait cero hace un solo intento.
func Acquire(n int, dir string, wait time.Duration) (*Slot, error) {
	if n < 1 {
		return nil, fmt.Errorf("el cupo debe ser al menos 1, era %d", n)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		for i := 0; i < n; i++ {
			p := filepath.Join(dir, fmt.Sprintf("turno-%d.lock", i))
			if s, ok := tryAcquire(p); ok {
				return s, nil
			}
		}
		if !time.Now().Before(deadline) {
			return nil, ErrSinTurno
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func tryAcquire(path string) (*Slot, bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, false
	}
	// Dejar constancia de quién lo tiene, solo para diagnóstico.
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "pid=%d desde=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	f.Sync()
	return &Slot{f: f, path: path}, true
}
