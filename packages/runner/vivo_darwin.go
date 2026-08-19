//go:build darwin

package runner

import dwin "github.com/argalla/taskkeeper/packages/platform/darwin"

func vivo(pid int) bool { return dwin.Alive(pid) }
