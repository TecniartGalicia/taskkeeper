//go:build darwin

package darwin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Los agentes de launchd de TaskKeeper viven bajo este prefijo de etiqueta y en
// ~/Library/LaunchAgents. Todo lo que TaskKeeper crea lleva este prefijo; nada
// más se toca.
const prefijoLabel = "com.argalla.taskkeeper."

type EspecDisparador struct {
	Tipo     string // once | daily | weekly
	Inicio   time.Time
	Horas    []time.Time // varias horas por día (si vacío, se usa Inicio)
	Weekdays []int       // ISO: 1=lunes .. 7=domingo
}

func labelDe(taskID string) string { return prefijoLabel + taskID }

func rutaPlist(taskID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", labelDe(taskID)+".plist"), nil
}

// Register escribe el plist y lo carga con launchctl.
//
// A diferencia de Windows, launchd NO despierta el equipo: StartCalendarInterval
// ejecuta las ocurrencias perdidas cuando el Mac vuelve del reposo. Es la
// diferencia que el asistente declara al usuario (ver AvisoReactivacion).
func Register(taskID, comando, argumentos string, spec EspecDisparador) error {
	p, err := rutaPlist(taskID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	xml, err := renderPlist(labelDe(taskID), comando, argumentos, spec)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(xml), 0o644); err != nil {
		return err
	}
	// bootout+bootstrap es idempotente: recarga si ya existía.
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", uid+"/"+labelDe(taskID)).Run()
	out, err := exec.Command("launchctl", "bootstrap", uid, p).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w · %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Unregister(taskID string) error {
	p, err := rutaPlist(taskID)
	if err != nil {
		return err
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", uid+"/"+labelDe(taskID)).Run()
	return os.Remove(p)
}

func Exists(taskID string) bool {
	p, err := rutaPlist(taskID)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func NombreTarea(taskID string) string { return labelDe(taskID) }

// LanzarWorker arranca una ejecución a mano y devuelve el control al instante.
func LanzarWorker(worker, taskID string, cuando time.Time) error {
	cmd := exec.Command(worker, "--run", taskID, "--manual",
		"--occurrence", cuando.Format(time.RFC3339))
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// RegistrarReintento deja un agente puntual `<id>-retry` para reintentar por
// cuota a la hora indicada.
func RegistrarReintento(worker, taskID string, cuando time.Time) error {
	id := taskID + "-retry"
	args := "--run " + taskID + " --manual --retry-trigger " + id
	return Register(id, worker, args, EspecDisparador{Tipo: "once", Inicio: cuando})
}

// AvisoReactivacion dice la verdad de macOS: el equipo no se despierta.
func AvisoReactivacion() string {
	return "en macOS el equipo no se despierta para ejecutar la tarea; " +
		"se ejecutará cuando el Mac esté despierto"
}

func renderPlist(label, comando, argumentos string, spec EspecDisparador) (string, error) {
	var args []string
	args = append(args, comando)
	args = append(args, strings.Fields(argumentos)...)
	var argXML strings.Builder
	for _, a := range args {
		fmt.Fprintf(&argXML, "\n    <string>%s</string>", escaparXML(a))
	}

	cal, err := renderCalendario(spec)
	if err != nil {
		return "", err
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + escaparXML(label) + `</string>
  <key>ProgramArguments</key>
  <array>` + argXML.String() + `
  </array>
  <key>RunAtLoad</key><false/>
` + cal + `
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>ProcessType</key><string>Background</string>
</dict>
</plist>
`, nil
}

func renderCalendario(spec EspecDisparador) (string, error) {
	horas := spec.Horas
	if len(horas) == 0 {
		horas = []time.Time{spec.Inicio}
	}
	switch spec.Tipo {
	case "once":
		t := horas[0]
		return fmt.Sprintf(`  <key>StartCalendarInterval</key>
  <dict><key>Month</key><integer>%d</integer><key>Day</key><integer>%d</integer><key>Hour</key><integer>%d</integer><key>Minute</key><integer>%d</integer></dict>`,
			int(t.Month()), t.Day(), t.Hour(), t.Minute()), nil
	case "daily":
		var b strings.Builder
		b.WriteString("  <key>StartCalendarInterval</key>\n  <array>")
		for _, t := range horas {
			fmt.Fprintf(&b, "\n    <dict><key>Hour</key><integer>%d</integer><key>Minute</key><integer>%d</integer></dict>", t.Hour(), t.Minute())
		}
		b.WriteString("\n  </array>")
		return b.String(), nil
	case "weekly":
		if len(spec.Weekdays) == 0 {
			return "", fmt.Errorf("regla semanal sin días")
		}
		var b strings.Builder
		b.WriteString("  <key>StartCalendarInterval</key>\n  <array>")
		for _, d := range spec.Weekdays {
			// launchd: Weekday 0/7 = domingo, 1 = lunes. ISO 7 (domingo) → 0.
			w := d
			if w == 7 {
				w = 0
			}
			for _, t := range horas {
				fmt.Fprintf(&b, "\n    <dict><key>Weekday</key><integer>%d</integer><key>Hour</key><integer>%d</integer><key>Minute</key><integer>%d</integer></dict>", w, t.Hour(), t.Minute())
			}
		}
		b.WriteString("\n  </array>")
		return b.String(), nil
	}
	return "", fmt.Errorf("tipo de disparador desconocido: %q", spec.Tipo)
}

func escaparXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// CurrentUserPrincipal no aplica en macOS; se conserva por simetría de interfaz.
func CurrentUserPrincipal() (string, error) {
	if u := os.Getenv("USER"); u != "" {
		return u, nil
	}
	return "", nil
}
