//go:build windows

package windows

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// P-21. Dar de alta, editar y borrar una tarea no toca ninguna otra del sistema.
// Es la condición para que sea aceptable meter una tarea por cada tarea nuestra.
func TestAltaEdicionBajaNoTocaNadaAjeno(t *testing.T) {
	antes := inventarioDelSistema(t)

	id := "prueba-" + time.Now().UTC().Format("150405")
	spec := TriggerSpec{Tipo: "daily", Inicio: time.Date(2030, 1, 1, 3, 15, 0, 0, time.Local)}

	if err := Register(id, "cmd.exe", "/c exit 0", spec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { Unregister(id) })

	if !Exists(id) {
		t.Fatalf("la tarea no aparece tras registrarla")
	}

	// Editar es volver a registrar: debe reemplazar, no duplicar.
	spec.Inicio = time.Date(2030, 1, 1, 4, 30, 0, 0, time.Local)
	if err := Register(id, "cmd.exe", "/c exit 0", spec); err != nil {
		t.Fatalf("Register (edición): %v", err)
	}
	gestionadas, err := ListManaged()
	if err != nil {
		t.Fatalf("ListManaged: %v", err)
	}
	n := 0
	for _, g := range gestionadas {
		if g == id {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("tras editar hay %d copias de la tarea, debería haber 1", n)
	}

	if err := Unregister(id); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if Exists(id) {
		t.Fatalf("la tarea sigue tras borrarla")
	}

	despues := inventarioDelSistema(t)
	if d := diferencia(antes, despues); len(d) != 0 {
		t.Fatalf("el inventario del Programador cambió fuera de nuestra carpeta: %v", d)
	}
}

// El XML debe llevar el usuario resuelto de verdad. Si no, schtasks responde
// "Acceso denegado", que es un mensaje que lleva a la conclusión equivocada.
func TestElXMLLlevaUsuarioResuelto(t *testing.T) {
	uid, err := CurrentUserPrincipal()
	if err != nil {
		t.Fatalf("CurrentUserPrincipal: %v", err)
	}
	if uid == "" || strings.Contains(uid, "%") {
		t.Fatalf("usuario mal resuelto: %q", uid)
	}
	trig, _ := renderTriggers(TriggerSpec{Tipo: "daily", Inicio: time.Now()})
	xml := renderTaskXML(uid, trig, "cmd.exe", "/c exit 0")
	if !strings.Contains(xml, "<UserId>"+uid+"</UserId>") {
		t.Errorf("el XML no incrusta el usuario resuelto")
	}
	if strings.Contains(xml, "%USERNAME%") || strings.Contains(xml, "%USERDOMAIN%") {
		t.Errorf("el XML usa variables de entorno, que schtasks no expande")
	}
	if !strings.Contains(xml, "<WakeToRun>true</WakeToRun>") {
		t.Errorf("falta WakeToRun, que solo se puede fijar por XML")
	}
}

func TestDisparadoresGenerados(t *testing.T) {
	casos := []struct {
		spec    TriggerSpec
		contien string
		falla   bool
	}{
		{TriggerSpec{Tipo: "once", Inicio: time.Now()}, "<TimeTrigger>", false},
		{TriggerSpec{Tipo: "daily", Inicio: time.Now()}, "<ScheduleByDay>", false},
		{TriggerSpec{Tipo: "weekly", Inicio: time.Now(), Weekdays: []int{1, 5}}, "<Monday /><Friday />", false},
		{TriggerSpec{Tipo: "weekly", Inicio: time.Now()}, "", true},
		{TriggerSpec{Tipo: "cada-luna-llena", Inicio: time.Now()}, "", true},
	}
	for _, c := range casos {
		got, err := renderTriggers(c.spec)
		if c.falla {
			if err == nil {
				t.Errorf("%q debería fallar", c.spec.Tipo)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.spec.Tipo, err)
		} else if !strings.Contains(got, c.contien) {
			t.Errorf("%q no contiene %q", c.spec.Tipo, c.contien)
		}
	}
}

func TestCodificacionUTF16(t *testing.T) {
	b := utf16LEconBOM("AB")
	esperado := []byte{0xFF, 0xFE, 'A', 0x00, 'B', 0x00}
	if len(b) != len(esperado) {
		t.Fatalf("longitud %d, se esperaba %d", len(b), len(esperado))
	}
	for i := range esperado {
		if b[i] != esperado[i] {
			t.Fatalf("byte %d = %#x, se esperaba %#x", i, b[i], esperado[i])
		}
	}
}

// ---------- auxiliares ----------

func inventarioDelSistema(t *testing.T) map[string]bool {
	t.Helper()
	out, err := exec.Command("schtasks", "/Query", "/FO", "CSV", "/NH").Output()
	if err != nil {
		t.Skipf("no se pudo inventariar el Programador: %v", err)
	}
	m := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		nombre := strings.Trim(strings.SplitN(l, `","`, 2)[0], `"`)
		if strings.HasPrefix(nombre, `\`+CarpetaTareas) {
			continue // lo nuestro no cuenta
		}
		m[nombre] = true
	}
	return m
}

func diferencia(a, b map[string]bool) []string {
	var d []string
	for k := range a {
		if !b[k] {
			d = append(d, "desapareció: "+k)
		}
	}
	for k := range b {
		if !a[k] {
			d = append(d, "apareció: "+k)
		}
	}
	return d
}

// Varias horas producen varios <CalendarTrigger> dentro de la misma tarea.
func TestVariasHorasDosTriggers(t *testing.T) {
	spec := TriggerSpec{Tipo: "daily", Horas: []time.Time{
		time.Date(2030, 1, 1, 15, 0, 0, 0, time.Local),
		time.Date(2030, 1, 1, 20, 0, 0, 0, time.Local),
	}}
	got, err := renderTriggers(spec)
	if err != nil {
		t.Fatalf("renderTriggers: %v", err)
	}
	if n := strings.Count(got, "<CalendarTrigger>"); n != 2 {
		t.Fatalf("se esperaban 2 disparadores, hay %d: %s", n, got)
	}
	if !strings.Contains(got, "T15:00:00") || !strings.Contains(got, "T20:00:00") {
		t.Errorf("faltan las horas 15:00 y 20:00 en el XML: %s", got)
	}
}
