//go:build windows

package windows

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// PipeName construye el nombre del canal incluyendo el SID del usuario, para
// que dos sesiones simultáneas de Windows no compartan canal.
func PipeName(sid string) string {
	return `\\.\pipe\Argalla.AgentCalendar.` + sid
}

// CurrentUserSID devuelve el SID del usuario que ejecuta el proceso.
func CurrentUserSID() (string, error) {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("no se pudo leer el usuario del token: %w", err)
	}
	return u.User.Sid.String(), nil
}

// ListenIPC abre el canal con un descriptor de seguridad explícito.
//
//	D:P            DACL protegida, no hereda entradas del contenedor
//	(A;;GA;;;SID)  acceso total únicamente al usuario propietario
//
// Se usa Named Pipe y no un puerto en 127.0.0.1 justamente por esto: un puerto
// local es alcanzable desde cualquier página abierta en el navegador del
// usuario, y habría que defenderse con validación de Origin y un secreto en
// cabecera. El pipe no es direccionable desde el navegador, así que esa
// superficie no existe.
//
// La frontera real de aislamiento es la cuenta de usuario: otro proceso del
// mismo usuario sí puede conectarse, y ningún secreto compartido lo evitaría
// porque ese proceso también podría leerlo. Es la misma frontera que protege
// las credenciales de los propios CLI de agente.
func ListenIPC(sid string) (net.Listener, error) {
	return winio.ListenPipe(PipeName(sid), &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)", sid),
		MessageMode:        true,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}
