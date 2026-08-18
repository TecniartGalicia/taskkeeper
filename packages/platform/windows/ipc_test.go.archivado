//go:build windows

package windows

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

// P-04. Aislamiento del canal entre la extensión y el runner.
func TestCanalSoloParaElUsuario(t *testing.T) {
	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatalf("CurrentUserSID: %v", err)
	}
	if !strings.HasPrefix(sid, "S-1-") {
		t.Fatalf("SID con forma inesperada: %q", sid)
	}
	t.Logf("SID del usuario: %s", sid)
	t.Logf("canal: %s", PipeName(sid))

	l, err := ListenIPC(sid)
	if err != nil {
		t.Fatalf("ListenIPC: %v", err)
	}
	defer l.Close()

	// Eco mínimo, para probar el viaje de ida y vuelta.
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, err := bufio.NewReader(c).ReadString('\n')
		if err != nil {
			return
		}
		c.Write([]byte("eco:" + line))
	}()

	conn, err := winio.DialPipe(PipeName(sid), ptrDur(5*time.Second))
	if err != nil {
		t.Fatalf("el propietario no pudo conectarse a su propio canal: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hola\n")); err != nil {
		t.Fatalf("escritura: %v", err)
	}
	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("lectura: %v", err)
	}
	if resp != "eco:hola\n" {
		t.Errorf("respuesta = %q", resp)
	}
}

// El mismo nombre no admite dos servidores: sirve de segunda barrera de
// instancia única, además del mutex.
func TestCanalNoAdmiteDosServidores(t *testing.T) {
	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatalf("CurrentUserSID: %v", err)
	}
	l1, err := ListenIPC(sid)
	if err != nil {
		t.Fatalf("primer ListenIPC: %v", err)
	}
	defer l1.Close()

	l2, err := ListenIPC(sid)
	if err == nil {
		l2.Close()
		t.Fatalf("un segundo servidor pudo abrir el mismo canal: la instancia única no está protegida por el canal")
	}
	t.Logf("segundo servidor rechazado, como debe: %v", err)
}

// No se abre ningún puerto TCP: el canal no es alcanzable desde el navegador
// del usuario ni desde la red local. Es la propiedad que motivó descartar
// HTTP en 127.0.0.1.
func TestNoSeAbrePuertoTCP(t *testing.T) {
	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatalf("CurrentUserSID: %v", err)
	}
	l, err := ListenIPC(sid)
	if err != nil {
		t.Fatalf("ListenIPC: %v", err)
	}
	defer l.Close()

	if _, ok := l.Addr().(*net.TCPAddr); ok {
		t.Fatalf("el canal expone una dirección TCP: %v", l.Addr())
	}
	t.Logf("dirección del canal: %s (%T), sin socket TCP", l.Addr(), l.Addr())
}

func ptrDur(d time.Duration) *time.Duration { return &d }
