// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DataDog/datadog-agent/comp/core"
	"github.com/DataDog/datadog-agent/pkg/util/fxutil"
)

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "datadog.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestCheckYAMLSyntax(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantErr      bool
		errContains  string
		wantWarnings []string
	}{
		{
			name:    "valid yaml",
			content: "api_key: abcdef1234567890abcdef1234567890\nsite: datadoghq.com\n",
		},
		{
			name:    "empty file",
			content: "",
		},
		{
			name:    "comments only",
			content: "# This is a comment\n# Another comment\n",
		},
		{
			name:        "tab indentation causes parse error",
			content:     "apm_config:\n\tenabled: true\n",
			wantErr:     true,
			errContains: "invalid YAML syntax",
		},
		{
			name:         "leading tab triggers both warning and parse error",
			content:      "api_key: abc\n\t# tab causes parse failure\n",
			wantErr:      true,
			errContains:  "invalid YAML syntax",
			wantWarnings: []string{"tab characters found on line(s) 2"},
		},
		{
			name:        "missing colon",
			content:     "api_key abcdef1234567890abcdef1234567890\n",
			wantErr:     true,
			errContains: "invalid YAML syntax",
		},
		{
			name:        "bad indentation",
			content:     "apm_config:\n  enabled: true\n bad_key: val\n",
			wantErr:     true,
			errContains: "invalid YAML syntax",
		},
		{
			name:         "tab on first line triggers both warning and parse error",
			content:      "\tapi_key: abc\n",
			wantErr:      true,
			errContains:  "invalid YAML syntax",
			wantWarnings: []string{"tab characters found on line(s) 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFixture(t, tt.content)
			parsed, warnings, err := checkYAMLSyntax(path)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				// parsed may be nil for empty/comment-only files, that's fine
				_ = parsed
			}

			for _, expectedWarn := range tt.wantWarnings {
				found := false
				for _, w := range warnings {
					if strings.Contains(w, expectedWarn) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected warning containing %q, got: %v", expectedWarn, warnings)
			}
		})
	}
}

func TestFormatLineNumbers(t *testing.T) {
	assert.Equal(t, "1", formatLineNumbers([]int{1}))
	assert.Equal(t, "1, 3, 5", formatLineNumbers([]int{1, 3, 5}))
	assert.Equal(t, "1, 2, 3, 4, 5", formatLineNumbers([]int{1, 2, 3, 4, 5}))
	assert.Equal(t, "1, 2, 3, 4, 5 (+2 more)", formatLineNumbers([]int{1, 2, 3, 4, 5, 6, 7}))
}

func TestCheckFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "datadog.yaml")
	require.NoError(t, os.WriteFile(path, []byte("api_key: test\n"), 0640))

	assert.Empty(t, checkFilePermissions(path), "0640 should have no warning")

	require.NoError(t, os.Chmod(path, 0644))
	warn := checkFilePermissions(path)
	assert.NotEmpty(t, warn, "0644 (world-readable) should warn")
	assert.Contains(t, warn, "world-readable")
}

func TestApiHostForSite(t *testing.T) {
	assert.Equal(t, "api.datadoghq.com", apiHostForSite("datadoghq.com"))
	assert.Equal(t, "api.datadoghq.eu", apiHostForSite("datadoghq.eu"))
	assert.Equal(t, "api.ddog-gov.com", apiHostForSite("ddog-gov.com"))
	assert.Equal(t, "api.us3.datadoghq.com", apiHostForSite("us3.datadoghq.com"))
}

func TestCheckSchema(t *testing.T) {
	t.Run("nil parsed config returns no findings", func(t *testing.T) {
		findings := checkSchema(nil)
		assert.Empty(t, findings)
	})

	t.Run("valid config keys produce no errors", func(t *testing.T) {
		parsed := map[string]interface{}{
			"api_key":      "abcdef1234567890abcdef1234567890",
			"site":         "datadoghq.com",
			"logs_enabled": true,
		}
		findings := checkSchema(parsed)
		// Filter to only errors (non-warn)
		var errs []schemaFinding
		for _, f := range findings {
			if !f.warn {
				errs = append(errs, f)
			}
		}
		assert.Empty(t, errs, "known valid keys should produce no schema errors")
	})

	t.Run("unknown key produces a warning", func(t *testing.T) {
		parsed := map[string]interface{}{
			"api_key":            "abcdef1234567890abcdef1234567890",
			"totally_fake_key_x": "value",
		}
		findings := checkSchema(parsed)
		var found bool
		for _, f := range findings {
			if f.warn && strings.Contains(f.message, "totally_fake_key_x") {
				found = true
			}
		}
		assert.True(t, found, "unknown key should produce an additionalProperties warning")
	})

	t.Run("wrong type produces a non-warn finding", func(t *testing.T) {
		// logs_enabled expects boolean; passing a string should trigger a type error
		parsed := map[string]interface{}{
			"api_key":      "abcdef1234567890abcdef1234567890",
			"logs_enabled": "not-a-bool",
		}
		findings := checkSchema(parsed)
		var found bool
		for _, f := range findings {
			if !f.warn {
				found = true
				break
			}
		}
		assert.True(t, found, "wrong type should produce a non-warn (error) schema finding")
	})
}

func TestConfigCheckCommand(t *testing.T) {
	commands := []*cobra.Command{
		MakeCommand(func() GlobalParams {
			return GlobalParams{}
		}),
	}

	fxutil.TestOneShotSubcommand(t,
		commands,
		[]string{"config", "check"},
		runConfigCheck,
		func(cliParams *cliParams, _ core.BundleParams) {
			require.Empty(t, cliParams.args)
		})
}

func TestValidateAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		wantErr     bool
		wantMasked  string
		errContains string
	}{
		{
			name:       "valid lowercase hex key",
			key:        "abcdef1234567890abcdef1234567890",
			wantMasked: "****************************7890",
		},
		{
			name:       "valid uppercase hex key",
			key:        "ABCDEF1234567890ABCDEF1234567890",
			wantMasked: "****************************7890",
		},
		{
			name:       "valid mixed case hex key",
			key:        "aBcDeF1234567890aBcDeF12345678Ff",
			wantMasked: "****************************78Ff",
		},
		{
			name:        "empty key",
			key:         "",
			wantErr:     true,
			errContains: "no api_key found in config",
		},
		{
			name:        "key too short",
			key:         "tooshort",
			wantErr:     true,
			errContains: "got 8 chars, expected 32 hex characters",
		},
		{
			name:        "key too long",
			key:         "abcdef1234567890abcdef1234567890extra",
			wantErr:     true,
			errContains: "got 37 chars, expected 32 hex characters",
		},
		{
			name:        "non-hex characters",
			key:         "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			wantErr:     true,
			errContains: "got 32 chars, expected 32 hex characters",
		},
		{
			name:        "key with spaces",
			key:         "abcdef1234567890 bcdef1234567890",
			wantErr:     true,
			errContains: "got 32 chars, expected 32 hex characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masked, err := validateAPIKey(tt.key)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Empty(t, masked)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantMasked, masked)
			}
		})
	}
}
