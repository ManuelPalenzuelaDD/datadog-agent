// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf

package testprogs

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/DataDog/datadog-agent/pkg/dyninst/ir"
	"github.com/DataDog/datadog-agent/pkg/dyninst/rcjson"
)

type probeYaml struct {
	Binary string           `yaml:"binary"`
	Probes []map[string]any `yaml:"probes"`
}

// MustGetProbeDefinitions calls GetProbeDefinitions and checks for an error.
func MustGetProbeDefinitions(t testing.TB, name string) []ir.ProbeDefinition {
	probes, err := GetProbeDefinitions(name)
	require.NoError(t, err)
	return probes
}

// GetProbeDefinitions returns the probe definitions for binary of a given name.
func GetProbeDefinitions(name string) ([]ir.ProbeDefinition, error) {
	probes, err := getProbeDefinitions(name)
	if err != nil {
		return nil, fmt.Errorf("get probe definitions for %s: %w", name, err)
	}
	return probes, nil
}

func getProbeDefinitions(name string) ([]ir.ProbeDefinition, error) {
	state, err := getState()
	if err != nil {
		return nil, err
	}
	yamlData, err := os.ReadFile(path.Join(state.probesCfgsDir, name+".yaml"))
	if err != nil {
		return nil, err
	}
	var probeYaml probeYaml
	err = yaml.Unmarshal(yamlData, &probeYaml)
	if err != nil {
		return nil, err
	}
	var probes []ir.ProbeDefinition
	for _, probe := range probeYaml.Probes {
		probeBytes, err := json.Marshal(probe)
		if err != nil {
			return nil, err
		}
		probe, err := rcjson.UnmarshalProbe(probeBytes)
		if err != nil {
			return nil, err
		}
		if err := rcjson.Validate(probe); err != nil {
			return nil, fmt.Errorf("validate probe %s: %w", probe.GetID(), err)
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

// IssueTagPrefix is the prefix of the issue tag.
// Formats:
//   - issue:REASON - unconditional skip
//   - issue:REASON@arch=ARCH - skip on specific architecture
//   - issue:REASON@version=VERSION - skip on exact version match
//   - issue:REASON@version>=VERSION - skip when toolchain >= version
//   - issue:REASON@arch=ARCH,version=VERSION - skip on arch with exact version
//   - issue:REASON@arch=ARCH,version>=VERSION - skip on arch with version >=
const IssueTagPrefix = "issue:"

// GetIssueTag returns the issue tag for a probe definition.
// Returns the full tag value (including any conditions).
func GetIssueTag(p ir.ProbeDefinition) (string, bool) {
	tags := p.GetTags()
	index := slices.IndexFunc(tags, func(tag string) bool {
		return strings.HasPrefix(tag, IssueTagPrefix)
	})
	if index == -1 {
		return "", false
	}
	return tags[index][len(IssueTagPrefix):], true
}

// HasIssueTag returns true if the probe definition has an unconditional issue tag
// (i.e., an issue tag without @arch= or @version= conditions).
func HasIssueTag(p ir.ProbeDefinition) bool {
	tag, ok := GetIssueTag(p)
	if !ok {
		return false
	}
	// Unconditional if there's no @ condition
	return !strings.Contains(tag, "@")
}

// ShouldSkipForConfig returns true if the probe should be skipped for the given
// architecture and toolchain combination based on conditional issue tags.
// This checks issue tags with @arch=, @version= (exact), and/or @version>= conditions.
func ShouldSkipForConfig(p ir.ProbeDefinition, arch, toolchain string) bool {
	for _, tag := range p.GetTags() {
		if !strings.HasPrefix(tag, IssueTagPrefix) {
			continue
		}
		value := strings.TrimPrefix(tag, IssueTagPrefix)

		// Find the @ separator for conditions
		atIdx := strings.Index(value, "@")
		if atIdx == -1 {
			// Unconditional issue tag - handled by HasIssueTag
			continue
		}

		// Parse conditions after @
		conditions := value[atIdx+1:]
		if matchesIssueConditions(conditions, arch, toolchain) {
			return true
		}
	}
	return false
}

// matchesIssueConditions checks if the given arch and toolchain match the conditions.
// Conditions format:
//   - "arch=ARCH" - exact architecture match
//   - "version=VERSION" - exact version match
//   - "version>=VERSION" - version greater than or equal
//   - Combinations: "arch=ARCH,version=VERSION" or "arch=ARCH,version>=VERSION"
func matchesIssueConditions(conditions, arch, toolchain string) bool {
	var requireArch string
	var requireVersionExact string
	var requireVersionGeq string

	for _, cond := range strings.Split(conditions, ",") {
		cond = strings.TrimSpace(cond)
		if strings.HasPrefix(cond, "arch=") {
			requireArch = strings.TrimPrefix(cond, "arch=")
		} else if strings.HasPrefix(cond, "version>=") {
			requireVersionGeq = strings.TrimPrefix(cond, "version>=")
		} else if strings.HasPrefix(cond, "version=") {
			requireVersionExact = strings.TrimPrefix(cond, "version=")
		}
	}

	// Check arch condition
	if requireArch != "" && requireArch != arch {
		return false
	}

	// Check version conditions
	if requireVersionExact != "" && toolchain != requireVersionExact {
		return false
	}
	if requireVersionGeq != "" && toolchain < requireVersionGeq {
		return false
	}

	// At least one condition must be specified
	return requireArch != "" || requireVersionExact != "" || requireVersionGeq != ""
}
