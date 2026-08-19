package redact

import "testing"

func TestBorraSecretosConocidos(t *testing.T) {
	casos := []struct {
		nombre  string
		entrada string
		fuera   string // fragmento que NO debe quedar
	}{
		{"anthropic", `usa sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123 para todo`, "AbCdEfGhIj"},
		{"openai", `key=sk-proj1234567890abcdefghijklmnop`, "1234567890abcdef"},
		{"github pat", `token gho_16C7e42F292c6912E7710c838347Ae178B4a`, "16C7e42F292c"},
		{"github fino", `github_pat_11ABCDEFG0abcdefghij_klmnopqrstuvwxyz1234`, "abcdefghij"},
		{"aws", `AKIAIOSFODNN7EXAMPLE en el log`, "AKIAIOSFODNN7EXAMPLE"},
		{"jwt", `Authorization: Bearer eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0.SflKxwRJSMeK`, "SflKxwRJSMeK"},
		{"pem", `-----BEGIN RSA PRIVATE KEY-----`, "BEGIN RSA PRIVATE KEY"},
		{"clave generica", `  "api_key": "abcdef1234567890"`, "abcdef1234567890"},
		{"password", `password = superSecreto123`, "superSecreto123"},
	}
	for _, c := range casos {
		got := String(c.entrada)
		if contiene(got, c.fuera) {
			t.Errorf("%s: el secreto siguió presente: %q → %q", c.nombre, c.entrada, got)
		}
		if !contiene(got, "[redactado]") {
			t.Errorf("%s: no se marcó la redacción: %q", c.nombre, got)
		}
	}
}

func TestNoTocaTextoNormal(t *testing.T) {
	for _, s := range []string{
		`El módulo define suma(a, b) y resta(a, b).`,
		`{"type":"result","subtype":"success","num_turns":3}`,
		`revisa las dependencias del proyecto`,
		`commit f3af0ecabc123`,
	} {
		if got := String(s); got != s {
			t.Errorf("modificó texto sin secretos: %q → %q", s, got)
		}
	}
}

// Un payload JSON con un secreto sigue siendo JSON válido tras redactar.
func TestConservaJSONValido(t *testing.T) {
	in := `{"type":"log","text":"exporté OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwx y seguí"}`
	got := String(in)
	if contiene(got, "sk-abcdefghijklmnop") {
		t.Fatalf("no redactó dentro del JSON: %q", got)
	}
	if got[0] != '{' || got[len(got)-1] != '}' {
		t.Errorf("rompió la forma del JSON: %q", got)
	}
}

func TestBytesNoCopiaSiNoHaySecreto(t *testing.T) {
	b := []byte("texto limpio")
	if &Bytes(b)[0] != &b[0] {
		t.Errorf("copió sin necesidad cuando no había secretos")
	}
}

func contiene(s, sub string) bool {
	if sub == "" {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
