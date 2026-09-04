package main

import "testing"

func TestStartupModeOnlyForDedicatedArgument(t *testing.T) {
	if startupMode(nil) || startupMode([]string{"--other"}) {
		t.Fatal("manual launch unexpectedly entered startup mode")
	}
	if !startupMode([]string{"--startup"}) || !startupMode([]string{"--other", "--startup"}) {
		t.Fatal("startup argument was not recognized")
	}
}
