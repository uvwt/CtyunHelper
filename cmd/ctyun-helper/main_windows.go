//go:build windows

package main

import (
	"log"

	"github.com/uvwt/CtyunHelper/internal/app"
	"github.com/uvwt/CtyunHelper/internal/winui"
)

func main() {
	model := app.NewModel(app.State{Connection: app.ConnectionAuth})
	if err := winui.Run(model); err != nil {
		log.Fatal(err)
	}
}
