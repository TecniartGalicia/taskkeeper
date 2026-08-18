//go:build windows

package turns

import (
	"os"

	"golang.org/x/sys/windows"
)

// En Windows el bloqueo exclusivo se pide sobre el handle del fichero. Es el
// sistema quien lo libera si el proceso muere, que es justo la propiedad que
// hace fiable el reparto de turnos entre procesos independientes.
func lock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol,
	)
}

func unlock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
