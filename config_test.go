package main

import (
	"testing"
)

func TestCfgParse(t *testing.T) {
	var appConfig Config
	cfgPath := "config/cfg.json"
	if err := ParseConfig(cfgPath, &appConfig); err != nil {
		t.Fatalf("Failed to parse config: %s", err)
	}
}
