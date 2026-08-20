package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// §H1: el perfil controlado debe DENEGAR lo peligroso, para que el settings del
// usuario no pueda ampliar permisos en una ejecución desatendida.
func TestPerfilSettingsDeniega(t *testing.T) {
	aud := perfilSettings(PerfilAuditoria)
	for _, deny := range []string{"Edit", "Write", "Bash", "WebFetch", "WebSearch"} {
		if !strings.Contains(aud, `"`+deny+`"`) {
			t.Errorf("auditoría debería denegar %q; settings=%s", deny, aud)
		}
	}
	if !strings.Contains(aud, `"defaultMode":"default"`) {
		t.Errorf("auditoría debería fijar defaultMode default; settings=%s", aud)
	}

	iso := perfilSettings(PerfilAislado)
	for _, deny := range []string{"git push", "git merge", "gh:", "WebFetch", "WebSearch"} {
		if !strings.Contains(iso, deny) {
			t.Errorf("cambios_aislados debería denegar %q; settings=%s", deny, iso)
		}
	}
	// Bash general NO se deniega en aislado (hace falta para tests), solo los subcomandos.
	if strings.Contains(iso, `"Bash"`) {
		t.Errorf("cambios_aislados no debe denegar Bash entero; settings=%s", iso)
	}
}

func TestEscribirPerfil(t *testing.T) {
	dir := t.TempDir()
	ruta, err := EscribirPerfil(dir, PerfilAislado)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(ruta) != dir {
		t.Errorf("ruta fuera del dir: %s", ruta)
	}
	b, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "git push") {
		t.Errorf("el fichero no tiene la denegación esperada: %s", b)
	}
}
