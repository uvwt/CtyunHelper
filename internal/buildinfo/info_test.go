package buildinfo

import "testing"

func TestBuildInfoDefaults(t *testing.T) {
	if AppName != "CtyunHelper" || DisplayName != "天翼云电脑助手" || Author != "uvwt" {
		t.Fatalf("unexpected app metadata: %q %q %q", AppName, DisplayName, Author)
	}
	if RepositoryURL != "https://github.com/uvwt/CtyunHelper" {
		t.Fatalf("repository = %q", RepositoryURL)
	}
	if Version == "" {
		t.Fatal("version must not be empty")
	}
}
