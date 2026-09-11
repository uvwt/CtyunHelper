//go:build windows

package winui

import "testing"

func TestShouldShowMainWindow(t *testing.T) {
	tests := []struct {
		name          string
		startHidden   bool
		trayAvailable bool
		want          bool
	}{
		{name: "normal launch with tray", startHidden: false, trayAvailable: true, want: true},
		{name: "startup launch with tray", startHidden: true, trayAvailable: true, want: false},
		{name: "normal launch without tray", startHidden: false, trayAvailable: false, want: true},
		{name: "startup launch without tray", startHidden: true, trayAvailable: false, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldShowMainWindow(tt.startHidden, tt.trayAvailable); got != tt.want {
				t.Fatalf("shouldShowMainWindow(%v, %v) = %v, want %v", tt.startHidden, tt.trayAvailable, got, tt.want)
			}
		})
	}
}
