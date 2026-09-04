//go:build !windows

package winui

import (
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/app"
)

func Run(*app.Model) error {
	return fmt.Errorf("Windows UI 仅支持 Windows")
}
