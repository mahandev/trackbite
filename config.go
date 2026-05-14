package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds every piece of runtime configuration the app needs.
//
// We deliberately keep this small and flat. Anything that varies between
// machines (API keys, file paths, the camera index) lives here. Anything
// that's part of the program's identity (the OFF user-agent string, the
// SQLite schema) lives next to the code that uses it.
type Config struct {
	USDAKey      string // FoodData Central API key — required
	EdamamID     string // optional; empty disables Edamam
	EdamamKey    string
	HelperBin    string // path to the Swift trackweighd binary
	CameraIndex  int    // 0 = built-in FaceTime camera
	CacheDBPath  string // SQLite file for cached lookups
}

// LoadConfig reads ".env" (if present) into the process environment, then
// snapshots the relevant variables into a Config.
//
// We don't pull in github.com/joho/godotenv for this — the format we care
// about is "KEY=VALUE" lines and "#" comments. Forty lines of stdlib are
// easier to read than a dependency.
func LoadConfig(envPath string) (*Config, error) {
	if err := loadDotEnv(envPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", envPath, err)
	}

	cfg := &Config{
		USDAKey:     os.Getenv("USDA_API_KEY"),
		EdamamID:    os.Getenv("EDAMAM_APP_ID"),
		EdamamKey:   os.Getenv("EDAMAM_APP_KEY"),
		HelperBin:   getenvDefault("TRACKWEIGHD_BIN", "./trackweighd/bin/trackweighd"),
		CacheDBPath: getenvDefault("CACHE_DB", "./trackbite.db"),
	}
	cfg.CameraIndex, _ = strconv.Atoi(getenvDefault("CAMERA_INDEX", "0"))

	if cfg.USDAKey == "" {
		// Not fatal — Open Food Facts and the embedded IFCT table both
		// work without any keys. But we warn so the user notices.
		fmt.Fprintln(os.Stderr, "warn: USDA_API_KEY not set — generic-food fallback will be limited")
	}
	return cfg, nil
}

// loadDotEnv parses a simple KEY=VALUE file and injects every entry into
// the process environment via os.Setenv. Already-set env vars win, so
// e.g. `USDA_API_KEY=xxx ./trackbite` overrides the file.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Strip optional surrounding quotes — common .env style.
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
