//go:build windows

package main

import (
	"log"

	"github.com/uvwt/CtyunHelper/internal/winui"
)

func main() {
	if err := winui.Run(); err != nil {
		log.Fatal(err)
	}
}
