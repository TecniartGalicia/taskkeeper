// Package gitwt aísla cada ejecución en su propio worktree.
//
// Es la capacidad central de TaskKeeper y la que no tiene ningún competidor:
// todos los demás lanzan el agente sobre el directorio de trabajo del usuario.
// Aquí el agente no puede tocar el checkout principal ni aunque quiera.
package gitwt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNoEsRepo     = errors.New("el directorio no es un repositorio Git")
	ErrRamaNoExiste = errors.New("la rama base no existe")
	ErrNombreMalo   = errors.New("nombre de worktree no admitido")
)

type Preflight struct {
	GitRoot    string
	BaseCommit string
	Sucio      bool // hay cambios sin guardar en el checkout principal
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w · %s", strings.Join(args, " "), err,
			strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// raizGit resuelve la carpeta a su raíz de repositorio. Comparte la validación
// de existencia entre Comprobar (aislado) y ComprobarSuave (directo).
// Devuelve (raíz, esGit, err): err solo si la carpeta no existe; esGit=false
// cuando existe pero no es un repositorio Git (o git no pudo resolverlo).
func raizGit(ctx context.Context, workspace string) (string, bool, error) {
	if fi, err := os.Stat(workspace); err != nil || !fi.IsDir() {
		return "", false, fmt.Errorf("la ruta %q no existe o no es un directorio", workspace)
	}
	root, err := git(ctx, workspace, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false, nil
	}
	return root, true, nil
}

// Comprobar resuelve la raíz del repositorio y el commit exacto de la rama base.
//
// Se resuelve el COMMIT, no la rama: si alguien empuja algo a la rama entre el
// preflight y el arranque del agente, la base no debe moverse bajo los pies de
// la ejecución.
func Comprobar(ctx context.Context, workspace, ramaBase string) (*Preflight, error) {
	root, esGit, err := raizGit(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if !esGit {
		return nil, fmt.Errorf("%w: %s", ErrNoEsRepo, workspace)
	}
	commit, err := git(ctx, root, "rev-parse", "--verify", ramaBase+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRamaNoExiste, ramaBase)
	}
	estado, err := git(ctx, root, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	return &Preflight{GitRoot: root, BaseCommit: commit, Sucio: estado != ""}, nil
}

// ComprobarSuave valida una carpeta para el modo «en la conversación» (directo):
// solo exige que EXISTA. No hace falta que sea un repositorio Git, porque en ese
// modo no se crea worktree ni rama. Si además resulta ser un repo Git, se
// captura su raíz (por si la misma carpeta se comparte algún día con una tarea
// aislada); si no lo es, GitRoot queda vacío y no es un error. Nota: cualquier
// fallo de git al resolver la raíz se trata como «no es git», lo cual es
// correcto para este modo, que no depende de git.
func ComprobarSuave(ctx context.Context, workspace string) (*Preflight, error) {
	root, _, err := raizGit(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return &Preflight{GitRoot: root}, nil
}

var nombreValido = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

// NombreRama construye el nombre de la rama del worktree y valida el trozo que
// viene del nombre de la tarea, que lo escribe una persona.
func NombreRama(slugTarea string, cuando time.Time, sufijo string) (string, error) {
	// Rechazar antes de sanear. Sanear "../../etc" a "etc" lo vuelve inofensivo,
	// pero convierte en silencio algo que es un error o un intento de escape en
	// un nombre plausible. Es mejor negarse y que se vea. La contención de ruta
	// de Crear sigue estando como segunda barrera.
	if strings.ContainsAny(slugTarea, `/\`) || strings.Contains(slugTarea, "..") ||
		strings.Contains(slugTarea, ":") {
		return "", fmt.Errorf("%w: %q contiene separadores de ruta", ErrNombreMalo, slugTarea)
	}
	s := strings.ToLower(strings.TrimSpace(slugTarea))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" || !nombreValido.MatchString(s) {
		return "", fmt.Errorf("%w: %q", ErrNombreMalo, slugTarea)
	}
	return fmt.Sprintf("taskkeeper/%s/%s-%s", s, cuando.UTC().Format("20060102-1504"), sufijo), nil
}

type Worktree struct {
	Path   string
	Branch string
	Base   string
	root   string
}

// Crear levanta el worktree desde el commit resuelto en el preflight.
//
// Los cambios sin confirmar del checkout principal NO se copian: el agente
// trabaja sobre el commit, no sobre el escritorio a medias de nadie. El aviso
// se le da al usuario en el preflight.
func Crear(ctx context.Context, pf *Preflight, baseDir, rama string) (*Worktree, error) {
	destino := filepath.Join(baseDir, strings.ReplaceAll(rama, "/", "_"))
	abs, err := filepath.Abs(destino)
	if err != nil {
		return nil, err
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	// El destino tiene que quedar dentro del directorio gestionado: ni "..",
	// ni rutas absolutas coladas por el nombre.
	if !strings.HasPrefix(abs, baseAbs+string(os.PathSeparator)) {
		return nil, fmt.Errorf("%w: la ruta escaparía del directorio gestionado", ErrNombreMalo)
	}
	if err := os.MkdirAll(baseAbs, 0o755); err != nil {
		return nil, err
	}
	if _, err := git(ctx, pf.GitRoot, "worktree", "add", "-b", rama, abs, pf.BaseCommit); err != nil {
		return nil, err
	}
	return &Worktree{Path: abs, Branch: rama, Base: pf.BaseCommit, root: pf.GitRoot}, nil
}

// Cambios devuelve los ficheros tocados y el diff completo contra la base.
func (w *Worktree) Cambios(ctx context.Context) (ficheros []string, diff string, err error) {
	if _, err = git(ctx, w.Path, "add", "-A"); err != nil {
		return nil, "", err
	}
	lista, err := git(ctx, w.Path, "diff", "--cached", "--name-only", w.Base)
	if err != nil {
		return nil, "", err
	}
	if lista != "" {
		ficheros = strings.Split(lista, "\n")
	}
	diff, err = git(ctx, w.Path, "diff", "--cached", w.Base)
	return ficheros, diff, err
}

// Confirmar deja el trabajo del agente en un commit dentro de la rama del
// worktree. No toca la rama base ni hace push.
func (w *Worktree) Confirmar(ctx context.Context, mensaje string) (string, error) {
	if _, err := git(ctx, w.Path, "add", "-A"); err != nil {
		return "", err
	}
	if estado, _ := git(ctx, w.Path, "status", "--porcelain"); estado == "" {
		return "", nil // no hubo cambios
	}
	if _, err := git(ctx, w.Path, "-c", "user.name=TaskKeeper",
		"-c", "user.email=taskkeeper@argalla.local",
		"commit", "-m", mensaje); err != nil {
		return "", err
	}
	return git(ctx, w.Path, "rev-parse", "HEAD")
}

// Aceptar funde la rama del worktree en la rama base, EN LOCAL. Nunca hace push:
// esa decisión es siempre de una persona y en su propio momento.
func Aceptar(ctx context.Context, gitRoot, rama, ramaBase string) error {
	actual, err := git(ctx, gitRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if actual != ramaBase {
		return fmt.Errorf("el checkout principal está en %q y la tarea parte de %q; "+
			"cambia de rama antes de aceptar", actual, ramaBase)
	}
	if estado, _ := git(ctx, gitRoot, "status", "--porcelain"); estado != "" {
		return errors.New("el checkout principal tiene cambios sin guardar; guárdalos antes de aceptar")
	}
	_, err = git(ctx, gitRoot, "merge", "--no-ff", "-m", "TaskKeeper: acepta "+rama, rama)
	return err
}

// Descartar borra el worktree y su rama sin dejar rastro. Es lo que ocurre al
// rechazar en la bandeja.
func Descartar(ctx context.Context, gitRoot, path, rama string) error {
	if _, err := git(ctx, gitRoot, "worktree", "remove", "--force", path); err != nil {
		// Si el worktree ya no está, seguimos: lo que importa es no dejar la rama.
		os.RemoveAll(path)
	}
	if _, err := git(ctx, gitRoot, "branch", "-D", rama); err != nil {
		return err
	}
	_, err := git(ctx, gitRoot, "worktree", "prune")
	return err
}
