// Package scheduler calcula las ocurrencias de una tarea programada.
//
// Todo el cálculo se hace en UTC y de forma monótona: cada ocurrencia es
// estrictamente posterior a la anterior. De ahí salen dos propiedades que no
// dependen de la biblioteca de fechas:
//
//   - Una hora de pared repetida por el retraso de otoño produce una sola
//     ejecución, porque el segundo instante ya no es posterior al primero
//     dentro del mismo día.
//   - La idempotencia se apoya en el instante UTC, no en la hora local.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	// Empotra la base de datos IANA en el binario. Sin esto, en Windows
	// time.LoadLocation depende del mapeo del sistema, que es incompleto.
	_ "time/tzdata"
)

type RuleType string

const (
	RuleOnce   RuleType = "once"
	RuleDaily  RuleType = "daily"
	RuleWeekly RuleType = "weekly"
)

// Rule es la regla de recurrencia tal y como se guarda en scheduled_tasks.
type Rule struct {
	Type     RuleType `json:"type"`
	AtLocal  string   `json:"at_local,omitempty"` // "2026-08-25T11:00", solo once
	Time     string   `json:"time,omitempty"`     // "11:00", daily y weekly
	Weekdays []int    `json:"weekdays,omitempty"` // ISO-8601: 1=lunes .. 7=domingo
}

// Occurrence es un instante concreto ya resuelto contra la zona horaria.
type Occurrence struct {
	ScheduledForUTC time.Time // lo que se guarda y se compara
	RequestedLocal  string    // lo que pidió el usuario
	ResolvedLocal   string    // lo que realmente va a ocurrir
	AdjustedForDST  bool      // true si el cambio de hora movió el instante
}

const (
	maxLookaheadDays = 400
	localLayout      = "2006-01-02T15:04"
)

// Next devuelve la primera ocurrencia estrictamente posterior a after.
// Devuelve (nil, nil) cuando la regla ya no producirá más ocurrencias.
func Next(rule Rule, timezone string, after time.Time) (*Occurrence, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("zona horaria %q desconocida: %w", timezone, err)
	}
	after = after.UTC()

	if rule.Type == RuleOnce {
		y, mo, d, hh, mm, err := parseLocalDateTime(rule.AtLocal)
		if err != nil {
			return nil, err
		}
		occ := materialize(y, mo, d, hh, mm, loc)
		if occ.ScheduledForUTC.After(after) {
			return &occ, nil
		}
		return nil, nil
	}

	hh, mm, err := parseHourMinute(rule.Time)
	if err != nil {
		return nil, err
	}
	if rule.Type == RuleWeekly && len(rule.Weekdays) == 0 {
		return nil, fmt.Errorf("regla semanal sin días asignados")
	}

	cursor := after.In(loc)
	for i := 0; i <= maxLookaheadDays; i++ {
		day := cursor.AddDate(0, 0, i)
		if rule.Type == RuleWeekly && !containsInt(rule.Weekdays, isoWeekday(day)) {
			continue
		}
		occ := materialize(day.Year(), day.Month(), day.Day(), hh, mm, loc)
		if occ.ScheduledForUTC.After(after) {
			return &occ, nil
		}
	}
	return nil, nil
}

// materialize convierte una hora de pared local en un instante UTC y detecta si
// el cambio de horario la desplazó.
//
// Hora inexistente (adelanto de primavera): time.Date normaliza hacia adelante,
// así que la hora resuelta no coincide con la pedida y AdjustedForDST es true.
// Es el comportamiento que pide la especificación: siguiente hora válida,
// registrando el ajuste.
//
// Hora repetida (retraso de otoño): time.Date devuelve una de las dos. No se
// elige explícitamente cuál porque no hace falta: Next avanza día a día y exige
// un instante estrictamente posterior, así que la hora repetida produce una
// sola ejecución sea cual sea la que devuelva.
func materialize(y int, mo time.Month, d, hh, mm int, loc *time.Location) Occurrence {
	requested := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d", y, int(mo), d, hh, mm)
	t := time.Date(y, mo, d, hh, mm, 0, 0, loc)
	resolved := t.Format(localLayout)
	return Occurrence{
		ScheduledForUTC: t.UTC(),
		RequestedLocal:  requested,
		ResolvedLocal:   resolved,
		AdjustedForDST:  resolved != requested,
	}
}

// IdempotencyKey es la clave única de FR-016: una tarea, un instante.
func IdempotencyKey(taskID string, o Occurrence) string {
	return taskID + ":" + o.ScheduledForUTC.Format(time.RFC3339)
}

func isoWeekday(t time.Time) int {
	if w := int(t.Weekday()); w != 0 {
		return w
	}
	return 7
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func parseHourMinute(s string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("hora %q no tiene formato HH:MM", s)
	}
	hh, err1 := strconv.Atoi(parts[0])
	mm, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, fmt.Errorf("hora %q fuera de rango", s)
	}
	return hh, mm, nil
}

func parseLocalDateTime(s string) (int, time.Month, int, int, int, error) {
	// Se interpreta en UTC solo para descomponer los campos; la zona real se
	// aplica después en materialize.
	t, err := time.Parse(localLayout, strings.TrimSpace(s))
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("fecha local %q no tiene formato %s: %w", s, localLayout, err)
	}
	return t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), nil
}
