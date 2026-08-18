//go:build windows

package platform

import win "github.com/argalla/taskkeeper/packages/platform/windows"

func NuevoGrupo() (GrupoProcesos, error) {
	j, err := win.NewJob()
	if err != nil {
		return nil, err
	}
	return j, nil
}
