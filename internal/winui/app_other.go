//go:build !windows

package winui

import (
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/app"
)

func Run(func() (*app.Runtime, error), RunOptions) error {
	return fmt.Errorf("Windows UI 仅支持 Windows")
}

func ShowError(_, _ string) {}
