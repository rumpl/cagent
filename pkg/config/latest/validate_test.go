package latest

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestToolset_Validate_LSP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "valid lsp with command",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: lsp
        command: gopls
`,
			wantErr: "",
		},
		{
			name: "lsp missing command",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: lsp
`,
			wantErr: "lsp toolset requires a command to be set",
		},
		{
			name: "lsp with args",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: lsp
        command: gopls
        args:
          - -remote=auto
`,
			wantErr: "",
		},
		{
			name: "lsp with env",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: lsp
        command: gopls
        env:
          GOFLAGS: "-mod=vendor"
`,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestToolset_Validate_Sandbox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "valid shell with sandbox",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: shell
        sandbox:
          image: alpine:latest
          paths:
            - .
            - /tmp
`,
			wantErr: "",
		},
		{
			name: "shell sandbox with readonly path",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: shell
        sandbox:
          paths:
            - ./:rw
            - /config:ro
`,
			wantErr: "",
		},
		{
			name: "shell sandbox without paths",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: shell
        sandbox:
          image: alpine:latest
`,
			wantErr: "sandbox requires at least one path to be set",
		},
		{
			name: "sandbox on non-shell toolset",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: filesystem
        sandbox:
          paths:
            - .
`,
			wantErr: "sandbox can only be used with type 'shell'",
		},
		{
			name: "shell without sandbox is valid",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: shell
`,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestToolset_Validate_GitAI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "valid filesystem with gitai enabled",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: filesystem
        gitai: true
`,
			wantErr: "",
		},
		{
			name: "valid filesystem with gitai disabled",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: filesystem
        gitai: false
`,
			wantErr: "",
		},
		{
			name: "gitai on non-filesystem toolset",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: shell
        gitai: true
`,
			wantErr: "gitai can only be used with type 'filesystem'",
		},
		{
			name: "filesystem without gitai is valid",
			config: `
version: "3"
agents:
  root:
    model: "openai/gpt-4"
    toolsets:
      - type: filesystem
`,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
