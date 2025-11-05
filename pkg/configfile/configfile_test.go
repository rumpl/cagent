package configfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	return tempDir
}

func TestNewManager(t *testing.T) {
	tempDir := setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify config file was created
	configPath := filepath.Join(tempDir, DefaultConfigDir, DefaultConfigFile)
	assert.FileExists(t, configPath)

	// Verify default values
	assert.Equal(t, DiffViewSplit, manager.GetDiffView())

	// Verify config file content
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var cfg Config
	err = json.Unmarshal(data, &cfg)
	require.NoError(t, err)
	assert.Equal(t, DiffViewSplit, cfg.DiffView)
}

func TestNewManager_ExistingConfig(t *testing.T) {
	tempDir := setupTestDir(t)

	// Create config file with custom values
	configPath := filepath.Join(tempDir, DefaultConfigDir, DefaultConfigFile)
	err := os.MkdirAll(filepath.Dir(configPath), 0o755)
	require.NoError(t, err)

	customCfg := Config{DiffView: DiffViewUnified}
	data, err := json.MarshalIndent(customCfg, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o644)
	require.NoError(t, err)

	// Verify the file was written correctly
	verifyData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var verifyCfg Config
	err = json.Unmarshal(verifyData, &verifyCfg)
	require.NoError(t, err)
	require.Equal(t, DiffViewUnified, verifyCfg.DiffView, "Config file should contain unified")

	// Create manager
	manager, err := NewManager()
	require.NoError(t, err)
	require.NotNil(t, manager)

	// Verify custom values were loaded
	assert.Equal(t, DiffViewUnified, manager.GetDiffView())
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	assert.Equal(t, DiffViewSplit, cfg.DiffView)
}

func TestGetSetDiffView(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	// Test default value
	assert.Equal(t, DiffViewSplit, manager.GetDiffView())

	// Test setting to unified
	err = manager.SetDiffView(DiffViewUnified)
	require.NoError(t, err)
	assert.Equal(t, DiffViewUnified, manager.GetDiffView())

	// Test setting to split
	err = manager.SetDiffView(DiffViewSplit)
	require.NoError(t, err)
	assert.Equal(t, DiffViewSplit, manager.GetDiffView())
}

func TestSetDiffView_Invalid(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	originalView := manager.GetDiffView()

	// Test invalid values
	invalidValues := []string{"invalid", "side-by-side", "", "SPLIT", "UNIFIED"}
	for _, invalid := range invalidValues {
		err = manager.SetDiffView(invalid)
		require.Error(t, err)
		assert.Equal(t, originalView, manager.GetDiffView(), "Config should not change on invalid value")
	}
}

func TestSaveLoad(t *testing.T) {
	setupTestDir(t)

	// Create first manager
	manager1, err := NewManager()
	require.NoError(t, err)

	// Set values
	err = manager1.SetDiffView(DiffViewUnified)
	require.NoError(t, err)

	// Save
	err = manager1.Save()
	require.NoError(t, err)

	// Create second manager (should load saved values)
	manager2, err := NewManager()
	require.NoError(t, err)

	// Verify values persisted
	assert.Equal(t, DiffViewUnified, manager2.GetDiffView())
}

func TestGetConfig(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	err = manager.SetDiffView(DiffViewUnified)
	require.NoError(t, err)

	cfg := manager.GetConfig()
	assert.Equal(t, DiffViewUnified, cfg.DiffView)

	// Verify it's a copy (modifying returned config doesn't affect manager)
	cfg.DiffView = DiffViewSplit
	assert.Equal(t, DiffViewUnified, manager.GetDiffView())
}

func TestUpdateConfig(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	// Test valid update
	newCfg := Config{DiffView: DiffViewUnified}
	err = manager.UpdateConfig(newCfg)
	require.NoError(t, err)
	assert.Equal(t, DiffViewUnified, manager.GetDiffView())

	// Test invalid update
	invalidCfg := Config{DiffView: "invalid"}
	err = manager.UpdateConfig(invalidCfg)
	require.Error(t, err)
	assert.Equal(t, DiffViewUnified, manager.GetDiffView(), "Config should not change on invalid update")
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid split",
			config:  Config{DiffView: DiffViewSplit},
			wantErr: false,
		},
		{
			name:    "valid unified",
			config:  Config{DiffView: DiffViewUnified},
			wantErr: false,
		},
		{
			name:    "invalid empty",
			config:  Config{DiffView: ""},
			wantErr: true,
		},
		{
			name:    "invalid value",
			config:  Config{DiffView: "side-by-side"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConcurrentReads(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent reads
	for range numGoroutines {
		go func() {
			defer wg.Done()
			view := manager.GetDiffView()
			assert.NotEmpty(t, view)
			cfg := manager.GetConfig()
			assert.NotEmpty(t, cfg.DiffView)
		}()
	}

	wg.Wait()
}

func TestConcurrentWrites(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent writes alternating between two valid values
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()
			view := DiffViewSplit
			if idx%2 == 0 {
				view = DiffViewUnified
			}
			err := manager.SetDiffView(view)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Final value should be one of the valid values
	finalView := manager.GetDiffView()
	assert.Contains(t, []string{DiffViewSplit, DiffViewUnified}, finalView)
}

func TestConcurrentReadWrite(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	const numReaders = 50
	const numWriters = 50
	var wg sync.WaitGroup
	wg.Add(numReaders + numWriters)

	// Start readers
	for range numReaders {
		go func() {
			defer wg.Done()
			for range 10 {
				view := manager.GetDiffView()
				assert.Contains(t, []string{DiffViewSplit, DiffViewUnified}, view)
			}
		}()
	}

	// Start writers
	for i := range numWriters {
		go func(idx int) {
			defer wg.Done()
			for j := range 10 {
				view := DiffViewSplit
				if (idx+j)%2 == 0 {
					view = DiffViewUnified
				}
				err := manager.SetDiffView(view)
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentSaveLoad(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent saves and loads
	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()

			// Alternate between save and load
			if idx%2 == 0 {
				view := DiffViewUnified
				if idx%4 == 0 {
					view = DiffViewSplit
				}
				err := manager.SetDiffView(view)
				assert.NoError(t, err)
				err = manager.Save()
				assert.NoError(t, err)
			} else {
				err := manager.Load()
				assert.NoError(t, err)
				view := manager.GetDiffView()
				assert.Contains(t, []string{DiffViewSplit, DiffViewUnified}, view)
			}
		}(i)
	}

	wg.Wait()
}

func TestAtomicWrite(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	configPath := manager.configPath

	// Perform multiple saves
	for i := range 10 {
		view := DiffViewSplit
		if i%2 == 0 {
			view = DiffViewUnified
		}
		err := manager.SetDiffView(view)
		require.NoError(t, err)
		err = manager.Save()
		require.NoError(t, err)

		// Verify no .tmp file left behind
		tempPath := configPath + ".tmp"
		_, err = os.Stat(tempPath)
		assert.True(t, os.IsNotExist(err), "Temp file should not exist after save")

		// Verify config file is valid JSON
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		var cfg Config
		err = json.Unmarshal(data, &cfg)
		require.NoError(t, err)
	}
}

func TestConfigPath(t *testing.T) {
	tempDir := setupTestDir(t)

	path, err := getConfigPath()
	require.NoError(t, err)

	expectedPath := filepath.Join(tempDir, DefaultConfigDir, DefaultConfigFile)
	assert.Equal(t, expectedPath, path)
}

func TestEnsureConfigExists(t *testing.T) {
	tempDir := setupTestDir(t)

	configPath := filepath.Join(tempDir, DefaultConfigDir, DefaultConfigFile)

	// First call should create directory and file
	err := ensureConfigExists(configPath)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Dir(configPath))
	assert.FileExists(t, configPath)

	// Verify default content
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	require.NoError(t, err)
	assert.Equal(t, DiffViewSplit, cfg.DiffView)

	// Second call should not fail (idempotent)
	err = ensureConfigExists(configPath)
	require.NoError(t, err)
}

func TestLoad_InvalidJSON(t *testing.T) {
	tempDir := setupTestDir(t)

	configPath := filepath.Join(tempDir, DefaultConfigDir, DefaultConfigFile)
	err := os.MkdirAll(filepath.Dir(configPath), 0o755)
	require.NoError(t, err)

	// Write invalid JSON
	err = os.WriteFile(configPath, []byte("{invalid json}"), 0o644)
	require.NoError(t, err)

	// Attempt to create manager (should fail on init/load)
	manager, err := NewManager()
	require.Error(t, err)
	if err != nil {
		// Error can happen during init or load phase
		assert.True(t, 
			contains(err.Error(), "init") || contains(err.Error(), "load"),
			"error should mention init or load: %v", err)
	} else {
		assert.Nil(t, manager, "Manager should be nil when error occurs")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoad_InvalidConfig(t *testing.T) {
	tempDir := setupTestDir(t)

	configPath := filepath.Join(tempDir, DefaultConfigDir, DefaultConfigFile)
	err := os.MkdirAll(filepath.Dir(configPath), 0o755)
	require.NoError(t, err)

	// Write valid JSON but invalid config
	invalidCfg := Config{DiffView: "invalid"}
	data, err := json.MarshalIndent(invalidCfg, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o644)
	require.NoError(t, err)

	// Attempt to create manager (should fail on validation)
	_, err = NewManager()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load")
}

func TestSave_InvalidConfig(t *testing.T) {
	setupTestDir(t)

	manager, err := NewManager()
	require.NoError(t, err)

	// Manually set invalid config (bypassing SetDiffView)
	manager.config.DiffView = "invalid"

	// Attempt to save (should fail validation)
	err = manager.Save()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save")
}

func TestConfigError(t *testing.T) {
	err := &ConfigError{
		Op:  "test",
		Err: assert.AnError,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "config test")
	assert.Contains(t, errStr, assert.AnError.Error())
}
