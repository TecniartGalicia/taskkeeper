//go:build windows

package windows

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// Carpeta propia dentro del Programador de tareas. Todo lo que TaskKeeper crea
// vive aquí y solo aquí: nunca se toca nada fuera.
const CarpetaTareas = `Argalla\TaskKeeper`

// TriggerSpec es la parte del calendario que el sistema operativo sabe expresar
// por sí mismo. Lo que no cabe aquí lo resuelve packages/scheduler.
type TriggerSpec struct {
	Tipo     string // once | daily | weekly
	Inicio   time.Time
	Horas    []time.Time // varios StartBoundary, uno por hora del día (si vacío, se usa Inicio)
	Weekdays []int       // ISO: 1=lunes .. 7=domingo
}

// CurrentUserPrincipal devuelve DOMINIO\usuario.
//
// Hallazgo de la Fase 0: si el XML no lleva un <UserId> válido, schtasks falla
// con "Acceso denegado", que empuja a pedir elevación cuando no hace ninguna
// falta. Y no vale poner %USERDOMAIN%\%USERNAME%: schtasks NO expande variables
// dentro del XML, las toma como literales.
func CurrentUserPrincipal() (string, error) {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	cuenta, dominio, _, err := u.User.Sid.LookupAccount("")
	if err != nil {
		return "", err
	}
	if dominio == "" {
		return cuenta, nil
	}
	return dominio + `\` + cuenta, nil
}

// Register da de alta el disparador de una tarea. Es idempotente: /F reemplaza.
func Register(taskID, comando, argumentos string, spec TriggerSpec) error {
	uid, err := CurrentUserPrincipal()
	if err != nil {
		return fmt.Errorf("resolviendo el usuario: %w", err)
	}
	trig, err := renderTriggers(spec)
	if err != nil {
		return err
	}
	xml := renderTaskXML(uid, trig, comando, argumentos)

	tmp, err := os.CreateTemp("", "tk-*.xml")
	if err != nil {
		return err
	}
	ruta := tmp.Name()
	defer os.Remove(ruta)
	if _, err := tmp.Write(utf16LEconBOM(xml)); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	nombre := NombreTarea(taskID)
	out, err := exec.Command("schtasks", "/Create", "/TN", nombre, "/XML", ruta, "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Create %s: %w · %s", nombre, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Unregister(taskID string) error {
	out, err := exec.Command("schtasks", "/Delete", "/TN", NombreTarea(taskID), "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Delete: %w · %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Exists(taskID string) bool {
	return exec.Command("schtasks", "/Query", "/TN", NombreTarea(taskID)).Run() == nil
}

func NombreTarea(taskID string) string { return CarpetaTareas + `\` + taskID }

// ListManaged devuelve solo los identificadores de nuestra carpeta. Sirve para
// detectar disparadores huérfanos sin mirar el resto del Programador.
func ListManaged() ([]string, error) {
	out, err := exec.Command("schtasks", "/Query", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	prefijo := `\` + CarpetaTareas + `\`
	for _, linea := range strings.Split(string(out), "\n") {
		linea = strings.TrimSpace(strings.Trim(strings.TrimSpace(linea), `"`))
		if linea == "" {
			continue
		}
		nombre := strings.SplitN(linea, `","`, 2)[0]
		if strings.HasPrefix(nombre, prefijo) {
			ids = append(ids, filepath.Base(nombre))
		}
	}
	return ids, nil
}

// ---------- generación del XML ----------

// renderTriggers emite un <Trigger> por cada hora del día. Un disparador por
// hora es como el Programador de Windows expresa "todos los días a las 15:00 y a
// las 20:00": dos CalendarTrigger diarios con distinto StartBoundary.
func renderTriggers(s TriggerSpec) (string, error) {
	inicios := s.Horas
	if len(inicios) == 0 {
		inicios = []time.Time{s.Inicio}
	}
	var b strings.Builder
	for i, ini := range inicios {
		one, err := renderTrigger(s.Tipo, ini, s.Weekdays)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(one)
	}
	return b.String(), nil
}

func renderTrigger(tipo string, inicioT time.Time, weekdays []int) (string, error) {
	inicio := inicioT.Format("2006-01-02T15:04:05")
	switch tipo {
	case "once":
		return `    <TimeTrigger>
      <Enabled>true</Enabled>
      <StartBoundary>` + inicio + `</StartBoundary>
    </TimeTrigger>`, nil
	case "daily":
		return `    <CalendarTrigger>
      <Enabled>true</Enabled>
      <StartBoundary>` + inicio + `</StartBoundary>
      <ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>
    </CalendarTrigger>`, nil
	case "weekly":
		if len(weekdays) == 0 {
			return "", fmt.Errorf("regla semanal sin días")
		}
		var dias bytes.Buffer
		nombres := map[int]string{1: "Monday", 2: "Tuesday", 3: "Wednesday",
			4: "Thursday", 5: "Friday", 6: "Saturday", 7: "Sunday"}
		for _, d := range weekdays {
			n, ok := nombres[d]
			if !ok {
				return "", fmt.Errorf("día ISO fuera de rango: %d", d)
			}
			fmt.Fprintf(&dias, "<%s />", n)
		}
		return `    <CalendarTrigger>
      <Enabled>true</Enabled>
      <StartBoundary>` + inicio + `</StartBoundary>
      <ScheduleByWeek><WeeksInterval>1</WeeksInterval><DaysOfWeek>` + dias.String() + `</DaysOfWeek></ScheduleByWeek>
    </CalendarTrigger>`, nil
	}
	return "", fmt.Errorf("tipo de disparador desconocido: %q", tipo)
}

func renderTaskXML(userID, trigger, comando, argumentos string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>TaskKeeper de Argalla. Gestionada por la extension; no editar a mano.</Description>
  </RegistrationInfo>
  <Triggers>
` + trigger + `
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + escaparXML(userID) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <WakeToRun>true</WakeToRun>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <AllowHardTerminate>true</AllowHardTerminate>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + escaparXML(comando) + `</Command>
      <Arguments>` + escaparXML(argumentos) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

func escaparXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// schtasks /XML espera UTF-16 con marca de orden de bytes.
func utf16LEconBOM(s string) []byte {
	u := utf16.Encode([]rune(s))
	buf := new(bytes.Buffer)
	buf.Write([]byte{0xFF, 0xFE})
	for _, v := range u {
		binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}
