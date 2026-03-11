// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/DataDog/datadog-agent/comp/core/config"
	log "github.com/DataDog/datadog-agent/comp/core/log/def"
)

//go:embed datadog-agent.schema.json
var agentSchemaJSON []byte

var (
	schemaOnce     sync.Once
	compiledSchema *jsonschema.Schema
	schemaErr      error
)

var validSites = map[string]string{
	"datadoghq.com":     "US1",
	"us3.datadoghq.com": "US3",
	"us5.datadoghq.com": "US5",
	"datadoghq.eu":      "EU1",
	"ap1.datadoghq.com": "AP1",
	"ap2.datadoghq.com": "AP2",
	"ddog-gov.com":      "US1-FED",
}

var apiKeyRegex = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func getSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(agentSchemaJSON))
		if err != nil {
			schemaErr = fmt.Errorf("could not parse config schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("datadog-agent.schema.json", doc); err != nil {
			schemaErr = fmt.Errorf("could not load config schema: %w", err)
			return
		}
		compiledSchema, schemaErr = c.Compile("datadog-agent.schema.json")
	})
	return compiledSchema, schemaErr
}

// checkFilePermissions warns if the config file is world-readable.
// Returns a warning string (empty if permissions look good).
func checkFilePermissions(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.Mode()&0o007 != 0 {
		return fmt.Sprintf("config file is world-readable (mode %s) — API key may be exposed. Fix: sudo chmod 640 %s", info.Mode(), path)
	}
	return ""
}

// checkYAMLSyntax reads the config file at path, checks for tab characters, and
// validates that the file is parseable YAML. Returns the parsed config map, a
// slice of non-fatal warnings (e.g. tab indentation), and an error if the YAML
// is structurally invalid.
func checkYAMLSyntax(path string) (map[string]interface{}, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}

	var warnings []string
	lines := strings.Split(string(data), "\n")
	var tabLines []int
	for i, line := range lines {
		if strings.HasPrefix(line, "\t") {
			tabLines = append(tabLines, i+1)
		}
	}
	if len(tabLines) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"tab characters found on line(s) %s — YAML requires spaces for indentation",
			formatLineNumbers(tabLines),
		))
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		fix := "check indentation (use spaces, not tabs) and colon placement"
		msg := err.Error()
		switch {
		case strings.Contains(msg, "found character that cannot start any token"):
			fix = "you likely have a tab character — YAML requires spaces for indentation"
		case strings.Contains(msg, "mapping values are not allowed"):
			fix = "check for missing spaces after colons or incorrect indentation"
		case strings.Contains(msg, "did not find expected key"):
			fix = "check indentation — a nested key may be at the wrong level"
		}
		return nil, warnings, fmt.Errorf("invalid YAML syntax: %s\n  fix: %s", msg, fix)
	}

	return parsed, warnings, nil
}

// validateAPIKey validates the format of a Datadog API key.
// Returns a masked display string on success, or an error describing the problem.
func validateAPIKey(apiKey string) (string, error) {
	if apiKey == "" {
		return "", errors.New("no api_key found in config")
	}
	if !apiKeyRegex.MatchString(apiKey) {
		return "", fmt.Errorf("api_key format is invalid (got %d chars, expected 32 hex characters)", len(apiKey))
	}
	masked := strings.Repeat("*", 28) + apiKey[28:]
	return masked, nil
}

// checkSite validates the configured site value. Returns the site string (defaulting to
// datadoghq.com if unset) and whether the site is a known valid value.
func checkSite(cfg config.Component) (site string, valid bool) {
	site = cfg.GetString("site")
	if site == "" {
		fmt.Printf("[WARN] site: no 'site' configured — defaulting to datadoghq.com (US1). Set explicitly if you're on a different region.\n")
		return "datadoghq.com", true
	}
	if region, known := validSites[site]; known {
		fmt.Printf("[OK]   site: '%s' is valid (region: %s)\n", site, region)
		return site, true
	}
	knownList := make([]string, 0, len(validSites))
	for s := range validSites {
		knownList = append(knownList, s)
	}
	fmt.Printf("[ERR]  site: unknown site '%s'. Valid sites: %s\n", site, strings.Join(knownList, ", "))
	return site, false
}

// checkAPIKeyLive calls the Datadog API to verify the key is accepted for the given site.
func checkAPIKeyLive(apiKey, site string) error {
	apiHost := apiHostForSite(site)
	url := fmt.Sprintf("https://%s/api/v1/validate", apiHost)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("could not build validation request: %w", err)
	}
	req.Header.Set("DD-API-KEY", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[WARN] api_validate: could not reach %s to validate API key (network error: %s)\n", apiHost, err)
		return nil
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Printf("[OK]   api_validate: API key is valid for site %s\n", site)
	case http.StatusForbidden:
		return fmt.Errorf("API key rejected by %s (HTTP 403) — wrong key or wrong region. Verify your key matches this site", site)
	default:
		fmt.Printf("[WARN] api_validate: unexpected response from %s: HTTP %d\n", apiHost, resp.StatusCode)
	}
	return nil
}

type schemaFinding struct {
	warn    bool
	path    string
	message string
}

// checkSchema validates the parsed config map against the embedded JSON schema.
// Returns a list of findings (errors and warnings).
func checkSchema(parsed map[string]interface{}) []schemaFinding {
	if parsed == nil {
		return nil
	}
	sch, err := getSchema()
	if err != nil {
		return []schemaFinding{{warn: true, message: fmt.Sprintf("could not load config schema: %s", err)}}
	}

	verr := sch.Validate(parsed)
	if verr == nil {
		return nil
	}

	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		return []schemaFinding{{warn: true, message: verr.Error()}}
	}

	basic := ve.BasicOutput()
	if len(basic.Errors) == 0 {
		return nil
	}

	var findings []schemaFinding
	for _, unit := range basic.Errors {
		if unit.Error == nil {
			continue
		}
		// Skip generic parent-level aggregation errors.
		switch unit.Error.Kind.(type) {
		case *kind.Schema, *kind.Group, *kind.Reference:
			continue
		}

		path := unit.InstanceLocation
		if path == "" {
			path = "(root)"
		}
		msg := unit.Error.String()

		// additionalProperties violations = unknown key (warn, not error — may be a future/custom key)
		_, isUnknownKey := unit.Error.Kind.(*kind.AdditionalProperties)
		findings = append(findings, schemaFinding{warn: isUnknownKey, path: path, message: msg})
	}
	return findings
}

// checkEnabledProducts prints a summary of which Datadog products are enabled.
func checkEnabledProducts(cfg config.Component) {
	products := []struct {
		key  string
		name string
	}{
		{"logs_enabled", "Log collection"},
		{"apm_config.enabled", "APM"},
		{"process_config.process_collection.enabled", "Live Process"},
		{"runtime_security_config.enabled", "Cloud Workload Security"},
		{"compliance_config.enabled", "Cloud Security Posture Management"},
		{"network_path.enabled", "Network Path"},
	}

	fmt.Printf("[INFO] products: enabled product summary:\n")
	for _, p := range products {
		if cfg.GetBool(p.key) {
			fmt.Printf("         ✓ %s\n", p.name)
		} else {
			fmt.Printf("         - %s (disabled)\n", p.name)
		}
	}
}

func runConfigCheck(_ log.Component, cfg config.Component, cliParams *cliParams) error {
	configPath := cfg.ConfigFileUsed()
	hasErrors := false

	// 1. File permissions
	if warn := checkFilePermissions(configPath); warn != "" {
		fmt.Printf("[WARN] permissions: %s\n", warn)
	} else {
		fmt.Printf("[OK]   permissions: file permissions look good\n")
	}

	// 2. YAML syntax (also detects tabs)
	parsed, yamlWarnings, err := checkYAMLSyntax(configPath)
	for _, w := range yamlWarnings {
		fmt.Printf("[WARN] yaml_syntax: %s\n", w)
	}
	if err != nil {
		fmt.Printf("[ERR]  yaml_syntax: %s\n", err)
		return err // cannot continue without valid YAML
	}
	fmt.Printf("[OK]   yaml_syntax: YAML syntax is valid\n")

	// 3. API key format
	apiKey := cfg.GetString("api_key")
	masked, err := validateAPIKey(apiKey)
	if err != nil {
		fmt.Printf("[ERR]  api_key: %s\n", err)
		hasErrors = true
	} else {
		fmt.Printf("[OK]   api_key: API key format is valid (%s)\n", masked)
	}

	// 4. Site validation
	site, siteValid := checkSite(cfg)

	// 5. Live API key validation (skip if --no-api, or if key/site are invalid)
	if !cliParams.noAPICheck && !hasErrors && siteValid {
		if err := checkAPIKeyLive(apiKey, site); err != nil {
			fmt.Printf("[ERR]  api_validate: %s\n", err)
			hasErrors = true
		}
	}

	// 6. Schema validation
	schemaFindings := checkSchema(parsed)
	if len(schemaFindings) == 0 {
		fmt.Printf("[OK]   schema: all config keys and value types are valid\n")
	}
	for _, f := range schemaFindings {
		if f.warn {
			fmt.Printf("[WARN] schema: unknown key at '%s': %s\n", f.path, f.message)
		} else {
			fmt.Printf("[ERR]  schema: invalid value at '%s': %s\n", f.path, f.message)
			hasErrors = true
		}
	}

	// 7. Product enablement summary
	checkEnabledProducts(cfg)

	if hasErrors {
		return errors.New("agent config check found errors — see output above")
	}
	return nil
}

func apiHostForSite(site string) string {
	switch site {
	case "datadoghq.com":
		return "api.datadoghq.com"
	case "datadoghq.eu":
		return "api.datadoghq.eu"
	case "ddog-gov.com":
		return "api.ddog-gov.com"
	default:
		return "api." + site
	}
}

func formatLineNumbers(lines []int) string {
	const maxShown = 5
	if len(lines) <= maxShown {
		parts := make([]string, len(lines))
		for i, l := range lines {
			parts[i] = strconv.Itoa(l)
		}
		return strings.Join(parts, ", ")
	}
	parts := make([]string, maxShown)
	for i := 0; i < maxShown; i++ {
		parts[i] = strconv.Itoa(lines[i])
	}
	return strings.Join(parts, ", ") + " (+" + strconv.Itoa(len(lines)-maxShown) + " more)"
}
