package scheduler

import (
	"fmt"
	"time"
)

// Previous devuelve la última ocurrencia prevista que NO es posterior a `at`.
//
// Es la pieza que le faltaba al worker. Cuando el sistema operativo lo lanza no
// le dice de qué hora viene, y suponer "ahora" tenía dos consecuencias malas:
// no se podía saber si la ejecución llegaba tarde, y la clave de idempotencia
// nunca coincidía con nada, así que la protección contra duplicados no protegía
// en las ejecuciones reales.
func Previous(rule Rule, timezone string, at time.Time) (*Occurrence, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("zona horaria %q desconocida: %w", timezone, err)
	}
	at = at.UTC()

	if rule.Type == RuleOnce {
		y, mo, d, hh, mm, err := parseLocalDateTime(rule.AtLocal)
		if err != nil {
			return nil, err
		}
		occ := materialize(y, mo, d, hh, mm, loc)
		if !occ.ScheduledForUTC.After(at) {
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

	cursor := at.In(loc)
	for i := 0; i <= maxLookaheadDays; i++ {
		day := cursor.AddDate(0, 0, -i)
		if rule.Type == RuleWeekly && !containsInt(rule.Weekdays, isoWeekday(day)) {
			continue
		}
		occ := materialize(day.Year(), day.Month(), day.Day(), hh, mm, loc)
		if !occ.ScheduledForUTC.After(at) {
			return &occ, nil
		}
	}
	return nil, nil
}

// ---------- política de ejecuciones perdidas ----------

type MisfirePolicy string

const (
	// Omitir: si se pasó la hora, no se ejecuta.
	MisfireSkip MisfirePolicy = "skip"
	// Ejecutar si el retraso no supera el máximo configurado.
	MisfireRunIfLate MisfirePolicy = "run_if_late"
	// Dejarla pendiente para que decida una persona.
	MisfireManual MisfirePolicy = "manual"
)

type MisfireDecision string

const (
	DecisionRun     MisfireDecision = "run"
	DecisionSkip    MisfireDecision = "skip"
	DecisionPending MisfireDecision = "pending"
)

// ResolveMisfire decide qué hacer con una ocurrencia según lo tarde que se
// llegue.
//
// Reparto de responsabilidades con el sistema operativo: Windows se encarga de
// despertarnos aunque sea tarde (StartWhenAvailable), y esta función decide si
// merece la pena ejecutar. Antes las dos políticas convivían sin hablarse y
// ganaba siempre la de Windows, así que la elección del usuario no servía de
// nada.
func ResolveMisfire(scheduledFor, now time.Time, p MisfirePolicy, maxLateness time.Duration) MisfireDecision {
	retraso := now.UTC().Sub(scheduledFor.UTC())

	// Un minuto de margen: el disparador del sistema no es un cronómetro, y
	// además aplica su propia dispersión de arranque.
	const margen = time.Minute
	if retraso <= margen {
		return DecisionRun
	}

	switch p {
	case MisfireRunIfLate:
		if retraso <= maxLateness {
			return DecisionRun
		}
		return DecisionSkip
	case MisfireManual:
		return DecisionPending
	default: // MisfireSkip y cualquier valor desconocido
		return DecisionSkip
	}
}

// CatchUp aplica la regla "como máximo una recuperación": recibe las ocurrencias
// perdidas y devuelve, a lo sumo, la última. Nunca se recuperan todas de golpe.
func CatchUp(perdidas []Occurrence, now time.Time, p MisfirePolicy, maxLateness time.Duration) *Occurrence {
	if len(perdidas) == 0 {
		return nil
	}
	ultima := perdidas[len(perdidas)-1]
	if ResolveMisfire(ultima.ScheduledForUTC, now, p, maxLateness) == DecisionRun {
		return &ultima
	}
	return nil
}
