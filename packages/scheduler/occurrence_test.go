package scheduler

import (
	"testing"
	"time"
)

const madrid = "Europe/Madrid"

func mustNext(t *testing.T, r Rule, tz string, after time.Time) *Occurrence {
	t.Helper()
	occ, err := Next(r, tz, after)
	if err != nil {
		t.Fatalf("Next devolvió error: %v", err)
	}
	if occ == nil {
		t.Fatalf("Next no devolvió ocurrencia")
	}
	return occ
}

// Criterio de aceptación 1 de la especificación: una tarea para el 25 de agosto
// de 2026 a las 11:00 en Europe/Madrid se ejecuta una sola vez.
// Agosto está en horario de verano, UTC+2, así que el instante es 09:00Z.
func TestCriterio1_TareaUnica(t *testing.T) {
	r := Rule{Type: RuleOnce, AtLocal: "2026-08-25T11:00"}
	occ := mustNext(t, r, madrid, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	if got, want := occ.ScheduledForUTC.Format(time.RFC3339), "2026-08-25T09:00:00Z"; got != want {
		t.Errorf("instante UTC = %s, se esperaba %s", got, want)
	}
	if occ.AdjustedForDST {
		t.Errorf("no debería haber ajuste de horario de verano en agosto")
	}

	// Y no hay una segunda ocurrencia.
	next, err := Next(r, madrid, occ.ScheduledForUTC)
	if err != nil {
		t.Fatalf("segunda llamada devolvió error: %v", err)
	}
	if next != nil {
		t.Errorf("una tarea única produjo una segunda ocurrencia: %+v", next)
	}
}

// Criterio de aceptación 2: una tarea semanal calcula correctamente sus cuatro
// próximas ocurrencias.
func TestCriterio2_SemanalCuatroOcurrencias(t *testing.T) {
	r := Rule{Type: RuleWeekly, Weekdays: []int{1}, Time: "09:00"} // lunes
	after := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)          // martes

	var got []string
	for i := 0; i < 4; i++ {
		occ := mustNext(t, r, madrid, after)
		got = append(got, occ.ResolvedLocal)
		after = occ.ScheduledForUTC
	}

	want := []string{
		"2026-08-24T09:00",
		"2026-08-31T09:00",
		"2026-09-07T09:00",
		"2026-09-14T09:00",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ocurrencia %d = %s, se esperaba %s", i+1, got[i], want[i])
		}
	}
}

// Hora inexistente: el 29 de marzo de 2026 en Europe/Madrid las 02:00 saltan a
// las 03:00, así que las 02:30 no existen. Se ejecuta en la siguiente hora
// válida y queda registrado el ajuste.
func TestDST_HoraInexistente(t *testing.T) {
	r := Rule{Type: RuleDaily, Time: "02:30"}
	occ := mustNext(t, r, madrid, time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC))

	if occ.RequestedLocal != "2026-03-29T02:30" {
		t.Errorf("hora pedida = %s", occ.RequestedLocal)
	}
	if occ.ResolvedLocal != "2026-03-29T03:30" {
		t.Errorf("hora resuelta = %s, se esperaba 2026-03-29T03:30", occ.ResolvedLocal)
	}
	if !occ.AdjustedForDST {
		t.Errorf("el ajuste de horario de verano no quedó registrado")
	}
}

// Hora repetida: el 25 de octubre de 2026 en Europe/Madrid las 03:00 vuelven a
// las 02:00, así que las 02:30 ocurren dos veces en el reloj de pared. La tarea
// debe dispararse una sola vez ese día.
func TestDST_HoraRepetidaUnaSolaVez(t *testing.T) {
	r := Rule{Type: RuleDaily, Time: "02:30"}
	after := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	enElDia25 := 0
	claves := map[string]bool{}
	for i := 0; i < 3; i++ {
		occ := mustNext(t, r, madrid, after)
		if occ.ResolvedLocal[:10] == "2026-10-25" {
			enElDia25++
		}
		k := IdempotencyKey("tarea-1", *occ)
		if claves[k] {
			t.Fatalf("clave de idempotencia repetida: %s", k)
		}
		claves[k] = true
		after = occ.ScheduledForUTC
	}

	if enElDia25 != 1 {
		t.Errorf("el día del retraso de hora produjo %d ocurrencias, se esperaba 1", enElDia25)
	}
}

// La zona horaria viene empotrada en el binario: no depende del mapeo de
// Windows, que no conoce los identificadores IANA.
func TestZonasEmpotradas(t *testing.T) {
	for _, tz := range []string{"Europe/Madrid", "America/New_York", "Asia/Tokyo", "Atlantic/Canary"} {
		if _, err := time.LoadLocation(tz); err != nil {
			t.Errorf("zona %s no disponible: %v", tz, err)
		}
	}
}

func TestReglasInvalidas(t *testing.T) {
	casos := []struct {
		nombre string
		rule   Rule
		tz     string
	}{
		{"zona desconocida", Rule{Type: RuleDaily, Time: "09:00"}, "Europe/Atlantida"},
		{"hora mal formada", Rule{Type: RuleDaily, Time: "25:99"}, madrid},
		{"semanal sin días", Rule{Type: RuleWeekly, Time: "09:00"}, madrid},
		{"fecha mal formada", Rule{Type: RuleOnce, AtLocal: "25/08/2026 11:00"}, madrid},
	}
	for _, c := range casos {
		if _, err := Next(c.rule, c.tz, time.Now().UTC()); err == nil {
			t.Errorf("%s: se esperaba error y no lo hubo", c.nombre)
		}
	}
}
