package main

func startupMode(args []string) bool {
	for _, arg := range args {
		if arg == "--startup" {
			return true
		}
	}
	return false
}
