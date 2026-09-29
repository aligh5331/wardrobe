package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ING-054: cmd/server no longer uses the stdlib "log" package and hands the
// configured logger to api.New through the ING-053 option.
func TestING054_Static_MainUsesSlogNotLog(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)

	for _, banned := range []string{`"log"`, "log.Printf(", "log.Fatalf(", "log.Fatal("} {
		if strings.Contains(src, banned) {
			t.Errorf("cmd/server/main.go contains %s, want slog only", banned)
		}
	}
	if !strings.Contains(src, "api.WithLogger(") {
		t.Error("cmd/server/main.go does not pass the logger with api.WithLogger(")
	}
}
