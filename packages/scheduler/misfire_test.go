package scheduler

import (
	"testing"
	"time"
)

// El worker tiene que poder saber "de qué hora vengo" sin que nadie se lo diga.
func TestPreviousDaDeQueHoraViene(t *testing.T) {
	r := Rule{Type: RuleDaily, Time: "03:00"}

	// Son las 09:00 locales del 25 de agosto: la última prevista fue hoy a las 03:00.
	// Agosto en Madrid es UTC+2, así que 03:00 local = 01:00Z.
	ahora := time.Date(2026, 8, 25, 7, 0, 0, 0, time.UTC) // 09:00 en Madrid
	occ, err := Previous(r, madrid, ahora)
	if err != nil {
		t.Fatalf("Previous: %v", err)
	}
	if got := occ.ScheduledForUTC.Format(time.RFC3339); got != "2026-08-25T01:00:00Z" {
		t.Errorf("ocurrencia = %s, se esperaba 2026-08-25T01:00:00Z", got)
	}

	// A las 02:00 locales del 25, la última prevista fue AYER a las 03:00.
	ahora = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) // 02:00 en Madrid
	occ, _ = Previous(r, madrid, ahora)
	if got := occ.ResolvedLocal; got != "2026-08-24T03:00" {
		t.Errorf("ocurrencia local = %s, se esperaba 2026-08-24T03:00", got)
	}
}

func TestPreviousSemanalYUnica(t *testing.T) {
	// Semanal los lunes: un miércoles, la última fue el lunes anterior.
	r := Rule{Type: RuleWeekly, Weekdays: []int{1}, Time: "09:00"}
	miercoles := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	occ, err := Previous(r, madrid, miercoles)
	if err != nil {
		t.Fatalf("Previous: %v", err)
	}
	if occ.ResolvedLocal[:10] != "2026-08-24" { // lunes 24
		t.Errorf("ocurrencia = %s, se esperaba el lunes 2026-08-24", occ.ResolvedLocal)
	}

	// Una tarea única cuya hora aún no ha llegado no tiene ocurrencia anterior.
	u := Rule{Type: RuleOnce, AtLocal: "2026-12-01T10:00"}
	if occ, _ := Previous(u, madrid, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)); occ != nil {
		t.Errorf("una tarea futura no debería tener ocurrencia anterior: %+v", occ)
	}
}

// Ida y vuelta: la ocurrencia anterior a la siguiente es ella misma. Es lo que
// hace que la clave de idempotencia sea estable entre un disparo normal y uno
// de recuperación.
func TestPreviousYNextSonCoherentes(t *testing.T) {
	r := Rule{Type: RuleDaily, Time: "03:00"}
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	siguiente, err := Next(r, madrid, base)
	if err != nil || siguiente == nil {
		t.Fatalf("Next: %v", err)
	}
	// Un instante después de la hora prevista, "la anterior" debe ser esa misma.
	anterior, err := Previous(r, madrid, siguiente.ScheduledForUTC.Add(time.Second))
	if err != nil || anterior == nil {
		t.Fatalf("Previous: %v", err)
	}
	if !anterior.ScheduledForUTC.Equal(siguiente.ScheduledForUTC) {
		t.Errorf("no coinciden: siguiente=%s anterior=%s",
			siguiente.ScheduledForUTC, anterior.ScheduledForUTC)
	}
}

func TestResolveMisfire(t *testing.T) {
	prevista := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)

	casos := []struct {
		nombre   string
		ahora    time.Time
		politica MisfirePolicy
		maximo   time.Duration
		espera   MisfireDecision
	}{
		{"a su hora se ejecuta", prevista, MisfireSkip, 0, DecisionRun},
		{"con medio minuto de margen se ejecuta", prevista.Add(30 * time.Second), MisfireSkip, 0, DecisionRun},
		{"omitir: tarde se omite", prevista.Add(3 * time.Hour), MisfireSkip, 0, DecisionSkip},
		{"si_tarde: dentro del plazo se ejecuta", prevista.Add(1 * time.Hour), MisfireRunIfLate, 2 * time.Hour, DecisionRun},
		{"si_tarde: fuera del plazo se omite", prevista.Add(5 * time.Hour), MisfireRunIfLate, 2 * time.Hour, DecisionSkip},
		{"manual: queda pendiente", prevista.Add(5 * time.Hour), MisfireManual, 0, DecisionPending},
		{"política desconocida: se omite, que es el lado seguro", prevista.Add(5 * time.Hour), "vete-a-saber", 0, DecisionSkip},
	}
	for _, c := range casos {
		if got := ResolveMisfire(prevista, c.ahora, c.politica, c.maximo); got != c.espera {
			t.Errorf("%s: dio %q, se esperaba %q", c.nombre, got, c.espera)
		}
	}
}

// Nunca se recuperan todas las ocurrencias perdidas de golpe: como máximo una.
func TestCatchUpRecuperaComoMaximoUna(t *testing.T) {
	base := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
	var perdidas []Occurrence
	for i := 0; i < 5; i++ {
		perdidas = append(perdidas, Occurrence{ScheduledForUTC: base.AddDate(0, 0, i)})
	}
	ahora := base.AddDate(0, 0, 4).Add(30 * time.Minute)

	got := CatchUp(perdidas, ahora, MisfireRunIfLate, 2*time.Hour)
	if got == nil {
		t.Fatalf("debería recuperar la última")
	}
	if !got.ScheduledForUTC.Equal(perdidas[4].ScheduledForUTC) {
		t.Errorf("recuperó %s en vez de la última", got.ScheduledForUTC)
	}

	// Con la política de omitir, no se recupera ninguna.
	if got := CatchUp(perdidas, ahora, MisfireSkip, 0); got != nil {
		t.Errorf("con omitir no debería recuperar nada, dio %+v", got)
	}
	if got := CatchUp(nil, ahora, MisfireRunIfLate, time.Hour); got != nil {
		t.Errorf("sin perdidas no hay nada que recuperar")
	}
}
