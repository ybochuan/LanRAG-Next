package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/cmd/desktop/agentrag"
	"github.com/joho/godotenv"
	"golang.org/x/sys/windows"
)

// The installer ships resources beside the executable. Development runs retain
// the original repository working directory and configuration behavior.
func prepareInstalledDesktop(executable string) (*installedDesktop, error) {
	appDir := filepath.Dir(executable)
	resources := filepath.Join(appDir, "resources")
	if _, err := os.Stat(filepath.Join(resources, "REVISION")); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	for _, path := range []string{
		filepath.Join(appDir, "extension-host.exe"),
		filepath.Join(appDir, "model-supervisor.exe"),
		filepath.Join(resources, "llama-runtime", "llama-server.exe"),
		filepath.Join(resources, "config", "config.yaml"),
		filepath.Join(resources, "web", "index.html"),
		filepath.Join(resources, "migrations", "sqlite"),
	} {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("installed Lite resources are incomplete: %w", err)
		}
	}
	local, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	profile := filepath.Join(local, "LanRAG Lite")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return nil, err
	}
	lockPath, err := windows.UTF16PtrFromString(filepath.Join(profile, "desktop.lock"))
	if err != nil {
		return nil, err
	}
	lock, err := windows.CreateFile(lockPath, windows.GENERIC_READ, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, errors.New("LanRAG Lite profile is already open or cannot be locked")
	}
	configFile := filepath.Join(profile, ".env.lite")
	if err := configureInstalledDesktop(profile, resources, configFile); err != nil {
		windows.CloseHandle(lock)
		return nil, err
	}
	host, err := agentrag.New(profile, filepath.Join(appDir, "extension-host.exe"))
	if err != nil {
		windows.CloseHandle(lock)
		return nil, err
	}
	supervisor := agentrag.NewModelSupervisor(profile, filepath.Join(appDir, "model-supervisor.exe"))
	return &installedDesktop{profile: profile, host: host, supervisor: supervisor, close: func() {
		_ = os.Remove(filepath.Join(profile, "core.url"))
		_ = windows.CloseHandle(lock)
	}}, nil
}

func configureInstalledDesktop(profile, resources, configFile string) error {
	if err := godotenv.Load(configFile); err != nil && !os.IsNotExist(err) {
		return errors.New("cannot read the LanRAG Lite profile .env.lite")
	}
	if err := os.Chdir(resources); err != nil {
		return err
	}
	settings := map[string]string{
		"AGENTRAG_BUILTIN_MCP":   "1",
		"DB_DRIVER":              "sqlite",
		"DB_PATH":                filepath.ToSlash(filepath.Join(profile, "weknora.db")),
		"RETRIEVE_DRIVER":        "sqlite",
		"STREAM_MANAGER_TYPE":    "memory",
		"STORAGE_TYPE":           "local",
		"LOCAL_STORAGE_BASE_DIR": filepath.Join(profile, "files"),
		"WEKNORA_WEB_DIR":        filepath.Join(resources, "web"),
		"LOG_PATH":               filepath.Join(profile, "core.log"),
		"XDG_CONFIG_HOME":        filepath.Join(profile, "preferences"),
	}
	// The packaged model manager publishes Windows inference endpoints here.
	settings["SSRF_WHITELIST_EXTRA"] = strings.Trim(os.Getenv("SSRF_WHITELIST_EXTRA")+",127.0.0.1", ",")
	if _, configured := os.LookupEnv("DUCKDB_SKIP_EXTENSION_LOAD"); !configured {
		settings["DUCKDB_SKIP_EXTENSION_LOAD"] = "1"
	}
	for key, value := range settings {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
