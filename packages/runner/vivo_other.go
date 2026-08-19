//go:build !windows && !darwin

package runner

// Sin soporte de plataforma no hay forma fiable de comprobar el proceso; se
// asume vivo para no cortar en falso. TaskKeeper no se distribuye aquí.
func vivo(pid int) bool { return true }
