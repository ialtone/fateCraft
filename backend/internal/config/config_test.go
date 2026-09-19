package config

import "testing"

func TestGetIntRangeUsesConfiguredValueAndRejectsOutOfRange(t *testing.T) {
	t.Setenv("FATECRAFT_TEST_CACHE_LIMIT", "32")
	if got := getintRange("FATECRAFT_TEST_CACHE_LIMIT", 16, 1, 64); got != 32 {
		t.Fatalf("getintRange() = %d, want 32", got)
	}
	t.Setenv("FATECRAFT_TEST_CACHE_LIMIT", "65")
	if got := getintRange("FATECRAFT_TEST_CACHE_LIMIT", 16, 1, 64); got != 16 {
		t.Fatalf("out-of-range getintRange() = %d, want fallback 16", got)
	}
}

func TestLoadReadsShareSigningKey(t *testing.T) {
	t.Setenv("SHARE_LINK_SIGNING_KEY", "independent-share-signing-key-1234")
	if got := Load().ShareLinkSigningKey; got != "independent-share-signing-key-1234" {
		t.Fatalf("ShareLinkSigningKey = %q", got)
	}
}

func TestLoadReadsDatabasePoolAndClampsInvalidValues(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "12")
	t.Setenv("DB_MAX_IDLE_CONNS", "4")
	t.Setenv("DB_CONN_MAX_LIFETIME", "45m")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "2m")
	config := Load()
	if config.DBMaxOpenConns != 12 || config.DBMaxIdleConns != 4 {
		t.Fatalf("database pool = open:%d idle:%d", config.DBMaxOpenConns, config.DBMaxIdleConns)
	}
	if config.DBConnMaxLifetime.String() != "45m0s" || config.DBConnMaxIdleTime.String() != "2m0s" {
		t.Fatalf("database durations = lifetime:%v idle:%v", config.DBConnMaxLifetime, config.DBConnMaxIdleTime)
	}

	t.Setenv("DB_MAX_OPEN_CONNS", "0")
	t.Setenv("DB_MAX_IDLE_CONNS", "-1")
	config = Load()
	if config.DBMaxOpenConns != 10 || config.DBMaxIdleConns != 3 {
		t.Fatalf("invalid database pool fallback = open:%d idle:%d", config.DBMaxOpenConns, config.DBMaxIdleConns)
	}
}
