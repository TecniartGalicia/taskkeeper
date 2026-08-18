package gitwt

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func repoDePrueba(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v · %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "Prueba")
	run("config", "user.email", "prueba@local")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("original\n"), 0o644)
	run("add", "-A")
	run("commit", "-qm", "inicial")
	return dir
}

func TestComprobarResuelveCommitYRama(t *testing.T) {
	ctx := context.Background()
	repo := repoDePrueba(t)

	pf, err := Comprobar(ctx, repo, "main")
	if err != nil {
		t.Fatalf("Comprobar: %v", err)
	}
	if len(pf.BaseCommit) != 40 {
		t.Errorf("commit base con forma rara: %q", pf.BaseCommit)
	}
	if pf.Sucio {
		t.Errorf("el repositorio recién creado no debería estar sucio")
	}

	if _, err := Comprobar(ctx, repo, "inexistente"); !errors.Is(err, ErrRamaNoExiste) {
		t.Errorf("una rama inexistente debería dar ErrRamaNoExiste, dio: %v", err)
	}
	if _, err := Comprobar(ctx, t.TempDir(), "main"); !errors.Is(err, ErrNoEsRepo) {
		t.Errorf("un directorio sin Git debería dar ErrNoEsRepo, dio: %v", err)
	}
}

func TestDetectaCambiosSinGuardar(t *testing.T) {
	ctx := context.Background()
	repo := repoDePrueba(t)
	os.WriteFile(filepath.Join(repo, "sucio.txt"), []byte("x"), 0o644)

	pf, err := Comprobar(ctx, repo, "main")
	if err != nil {
		t.Fatalf("Comprobar: %v", err)
	}
	if !pf.Sucio {
		t.Errorf("no detectó los cambios sin guardar del checkout principal")
	}
}

// P-27, primera mitad y razón de ser del producto: el agente trabaja en su copia
// y el checkout principal queda intacto.
func TestElCheckoutPrincipalQuedaIntacto(t *testing.T) {
	ctx := context.Background()
	repo := repoDePrueba(t)
	base := t.TempDir()

	pf, _ := Comprobar(ctx, repo, "main")
	rama, err := NombreRama("Mantenimiento Semanal", time.Now(), "abc123")
	if err != nil {
		t.Fatalf("NombreRama: %v", err)
	}
	wt, err := Crear(ctx, pf, base, rama)
	if err != nil {
		t.Fatalf("Crear: %v", err)
	}

	// El "agente" trabaja.
	os.WriteFile(filepath.Join(wt.Path, "README.md"), []byte("tocado por el agente\n"), 0o644)
	os.WriteFile(filepath.Join(wt.Path, "nuevo.txt"), []byte("nuevo\n"), 0o644)

	ficheros, diff, err := wt.Cambios(ctx)
	if err != nil {
		t.Fatalf("Cambios: %v", err)
	}
	if len(ficheros) != 2 {
		t.Errorf("ficheros tocados = %v, se esperaban 2", ficheros)
	}
	if !strings.Contains(diff, "tocado por el agente") {
		t.Errorf("el diff no refleja el cambio")
	}

	// Lo que importa: el original no se ha movido.
	orig, _ := os.ReadFile(filepath.Join(repo, "README.md"))
	if string(orig) != "original\n" {
		t.Fatalf("EL CHECKOUT PRINCIPAL FUE MODIFICADO: %q", orig)
	}
	if _, err := os.Stat(filepath.Join(repo, "nuevo.txt")); err == nil {
		t.Fatalf("apareció un fichero del agente en el checkout principal")
	}
}

// P-25. Aceptar funde en local; rechazar no deja rastro.
func TestAceptarYRechazar(t *testing.T) {
	ctx := context.Background()

	t.Run("aceptar funde sin push", func(t *testing.T) {
		repo := repoDePrueba(t)
		pf, _ := Comprobar(ctx, repo, "main")
		rama, _ := NombreRama("tarea", time.Now(), "a1")
		wt, err := Crear(ctx, pf, t.TempDir(), rama)
		if err != nil {
			t.Fatalf("Crear: %v", err)
		}
		os.WriteFile(filepath.Join(wt.Path, "nuevo.txt"), []byte("del agente\n"), 0o644)
		if _, err := wt.Confirmar(ctx, "trabajo del agente"); err != nil {
			t.Fatalf("Confirmar: %v", err)
		}
		if err := Aceptar(ctx, repo, rama, "main"); err != nil {
			t.Fatalf("Aceptar: %v", err)
		}
		if _, err := os.Stat(filepath.Join(repo, "nuevo.txt")); err != nil {
			t.Fatalf("tras aceptar, el fichero no llegó a la rama base: %v", err)
		}
	})

	t.Run("rechazar no deja rastro", func(t *testing.T) {
		repo := repoDePrueba(t)
		pf, _ := Comprobar(ctx, repo, "main")
		rama, _ := NombreRama("tarea", time.Now(), "b2")
		wt, err := Crear(ctx, pf, t.TempDir(), rama)
		if err != nil {
			t.Fatalf("Crear: %v", err)
		}
		os.WriteFile(filepath.Join(wt.Path, "basura.txt"), []byte("no\n"), 0o644)
		wt.Confirmar(ctx, "trabajo rechazado")

		if err := Descartar(ctx, repo, wt.Path, rama); err != nil {
			t.Fatalf("Descartar: %v", err)
		}
		if _, err := os.Stat(wt.Path); err == nil {
			t.Errorf("el worktree sigue en disco")
		}
		out, _ := exec.Command("git", "-C", repo, "branch", "--list", rama).Output()
		if strings.TrimSpace(string(out)) != "" {
			t.Errorf("la rama sigue existiendo tras rechazar: %q", out)
		}
		if _, err := os.Stat(filepath.Join(repo, "basura.txt")); err == nil {
			t.Errorf("el fichero rechazado llegó al checkout principal")
		}
	})
}

// El nombre de la tarea lo escribe una persona: no puede servir para escapar
// del directorio gestionado.
func TestNombresPeligrosos(t *testing.T) {
	for _, malo := range []string{"..", "../../etc", "C:\\Windows", "", "   ", "///"} {
		if _, err := NombreRama(malo, time.Now(), "x"); err == nil {
			t.Errorf("NombreRama aceptó %q", malo)
		}
	}
	bueno, err := NombreRama("Revisión de PR #1234", time.Now(), "x1")
	if err != nil {
		t.Fatalf("un nombre normal debería valer: %v", err)
	}
	if !strings.HasPrefix(bueno, "taskkeeper/revisi-n-de-pr-1234/") {
		t.Errorf("rama generada = %q", bueno)
	}
}

func TestCrearRechazaEscapes(t *testing.T) {
	ctx := context.Background()
	repo := repoDePrueba(t)
	pf, _ := Comprobar(ctx, repo, "main")
	if _, err := Crear(ctx, pf, t.TempDir(), "..\\..\\fuera"); err == nil {
		t.Errorf("Crear aceptó una rama que escapa del directorio gestionado")
	}
}
