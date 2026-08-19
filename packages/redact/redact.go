// Package redact borra secretos de cualquier texto antes de guardarlo.
//
// Se aplica en un ÚNICO sitio —el escritor de eventos de la base— para que
// ningún camino pueda saltárselo. La salida de un agente puede contener claves
// que leyó de un fichero de configuración; ni la base ni la interfaz deben
// llegar a verlas.
package redact

import "regexp"

// Patrones ordenados de más específico a más general. La sustitución conserva
// la longitud aproximada para no romper un JSON que la contenga.
var patrones = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{16,}`),                                       // Anthropic
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),                                              // OpenAI y similares
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),                                       // GitHub PAT clásico
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),                                     // GitHub PAT de grano fino
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),                                    // Slack
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                                 // AWS access key id
	regexp.MustCompile(`ya29\.[A-Za-z0-9_\-]{20,}`),                                        // Google OAuth
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), // JWT
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),                               // clave privada PEM
	// Genéricos: "authorization: Bearer …", "api_key = …". Se dejan para el
	// final porque son los más propensos a falsos positivos.
	regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._\-]{20,}`),
	regexp.MustCompile(`(?i)("?(?:api[_-]?key|token|secret|password|passwd|client[_-]?secret)"?\s*[=:]\s*"?)[^"\s,}]{8,}`),
}

const marca = "[redactado]"

// String borra los secretos que reconozca. Nunca falla y no toca lo que no
// coincide con un patrón.
func String(s string) string {
	for _, p := range patrones {
		if p.NumSubexp() > 0 {
			// Conserva el prefijo capturado (por ejemplo `api_key: `) y solo
			// tapa el valor, para que se entienda qué se redactó.
			s = p.ReplaceAllString(s, "${1}"+marca)
		} else {
			s = p.ReplaceAllString(s, marca)
		}
	}
	return s
}

// Bytes es la versión para payloads sin copiar de más cuando no hay secretos.
func Bytes(b []byte) []byte {
	r := String(string(b))
	if r == string(b) {
		return b
	}
	return []byte(r)
}
