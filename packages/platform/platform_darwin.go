//go:build darwin

package platform

import dwin "github.com/argalla/taskkeeper/packages/platform/darwin"

func NuevoGrupo() (GrupoProcesos, error) {
	j, err := dwin.NewJob()
	if err != nil {
		return nil, err
	}
	return j, nil
}
