package adapters

import (
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func lookupEnv(k string) (string, bool) { return os.LookupEnv(k) }

func exeSufijo() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// porVersion ordena rutas del tipo "…-2.1.233-win32-x64" por número de versión y
// no alfabéticamente: con orden alfabético, 2.1.9 quedaría por encima de 2.1.10.
type porVersion []string

var reNum = regexp.MustCompile(`\d+`)

func (p porVersion) Len() int      { return len(p) }
func (p porVersion) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p porVersion) Less(i, j int) bool {
	a, b := numeros(p[i]), numeros(p[j])
	for k := 0; k < len(a) && k < len(b); k++ {
		if a[k] != b[k] {
			return a[k] < b[k]
		}
	}
	return len(a) < len(b)
}

func numeros(s string) []int {
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	var out []int
	for _, m := range reNum.FindAllString(s, -1) {
		n, _ := strconv.Atoi(m)
		out = append(out, n)
	}
	return out
}
