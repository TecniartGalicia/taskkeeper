//go:build windows

package runner

import win "github.com/argalla/taskkeeper/packages/platform/windows"

func vivo(pid int) bool { return win.Alive(pid) }
