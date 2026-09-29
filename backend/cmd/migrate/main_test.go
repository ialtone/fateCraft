package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRejectedCommandsDoNotOpenDatabase(t *testing.T) {
	// Invalid configuration must never mask command validation, even with the old override.
	t.Setenv("DATABASE_URL", "invalid-dsn")
	t.Setenv("MIGRATION_TIMEOUT", "invalid")
	t.Setenv("ALLOW_MIGRATION_DOWN", "true")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"down", []string{"down"}, "baseline 00001 is irreversible"},
		{"unknown", []string{"reset"}, "unknown migration command"},
		{"extra arguments", []string{"up", "extra"}, "usage:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(context.Background(), tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("run(%v) = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestInvalidTimeoutPreventsDatabaseAccess(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid-dsn")
	for _, name := range []string{"MIGRATION_TIMEOUT", "MIGRATION_CONNECT_TIMEOUT"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MIGRATION_TIMEOUT", "10m")
			t.Setenv("MIGRATION_CONNECT_TIMEOUT", "10s")
			t.Setenv(name, "0s")
			err := run(context.Background(), nil)
			if err == nil || !strings.Contains(err.Error(), name+" must be a positive duration") {
				t.Fatalf("run = %v, want invalid timeout error", err)
			}
		})
	}
}

func TestDurationEnv(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", 10 * time.Minute, false},
		{"25s", 25 * time.Second, false},
		{"3m", 3 * time.Minute, false},
		{"0", 0, true},
		{"-1s", 0, true},
		{"forever", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("MIGRATION_TIMEOUT", tc.value)
			got, err := durationEnv("MIGRATION_TIMEOUT", 10*time.Minute)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("durationEnv(%q) = %v, %v; want %v, invalid=%v", tc.value, got, err, tc.want, tc.invalid)
			}
		})
	}
}
