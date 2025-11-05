package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go-micro.dev/v4/config"
	"go-micro.dev/v4/config/source/file"
)

type DiffViewValue string

const (
	DefaultConfigDir  = ".cagent"
	DefaultConfigFile = "config.json"

	DiffViewUnified DiffViewValue = "unified"
	DiffViewSplit   DiffViewValue = "split"
)

type Config struct {
	DiffView DiffViewValue `json:"diffView"`
}

type Manager struct {
	mu         sync.RWMutex
	config     Config
	configPath string
	saveMu     sync.Mutex
}

func NewManager() (*Manager, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	if err := ensureConfigExists(configPath); err != nil {
		return nil, err
	}

	m := &Manager{
		configPath: configPath,
		config:     defaultConfig(),
	}

	if err := m.Load(); err != nil {
		return nil, err
	}

	return m, nil
}

func defaultConfig() Config {
	return Config{
		DiffView: DiffViewSplit,
	}
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	return filepath.Join(home, DefaultConfigDir, DefaultConfigFile), nil
}

func ensureConfigExists(configPath string) error {
	dir := filepath.Dir(configPath)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := defaultConfig()
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal default config: %w", err)
		}

		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	return nil
}

func (m *Manager) Load() error {
	microConfig, err := config.NewConfig(
		config.WithSource(
			file.NewSource(
				file.WithPath(m.configPath),
			),
		),
	)
	if err != nil {
		return err
	}

	if err := microConfig.Load(); err != nil {
		return err
	}

	var cfg Config
	if err := microConfig.Scan(&cfg); err != nil {
		return err
	}

	m.mu.Lock()
	m.config = cfg
	m.mu.Unlock()

	return nil
}

func (m *Manager) Save() error {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	m.mu.RLock()
	configCopy := m.config
	m.mu.RUnlock()

	data, err := json.MarshalIndent(configCopy, "", "  ")
	if err != nil {
		return err
	}

	tempPath := m.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	if err := os.Rename(tempPath, m.configPath); err != nil {
		os.Remove(tempPath)
		return err
	}

	return nil
}

func (m *Manager) GetDiffView() DiffViewValue {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.DiffView
}

func (m *Manager) SetDiffView(view DiffViewValue) error {
	if view != DiffViewUnified && view != DiffViewSplit {
		return fmt.Errorf("invalid diff view: %s (must be %s or %s)", view, DiffViewUnified, DiffViewSplit)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.DiffView = view
	return nil
}

func (m *Manager) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) UpdateConfig(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
	return nil
}
