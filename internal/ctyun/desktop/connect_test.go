package desktop

import "testing"

func TestConnectionInfoValidateUsesCurrentClinkRoutingFields(t *testing.T) {
	valid := ConnectionInfo{
		DesktopID: 7, Host: "desktop.internal", Port: "443", ClinkLVSOutHost: "clink.example:443",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("current Clink routing fields should be sufficient: %v", err)
	}

	invalid := valid
	invalid.ClinkLVSOutHost = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("missing Clink host should fail validation")
	}
}
