package server_test

import (
	"testing"

	"yascore/config"
)

func exampleAssistant(t *testing.T) (string, []string) {
	t.Helper()
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Core.Name, append([]string(nil), cfg.Core.Wake.Words...)
}
