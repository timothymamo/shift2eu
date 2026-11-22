// Pins the one Renovate packageRule that reads infra/site's OpenTofu pin (issue #434, item 3). The terraform
// manager reports infra/site/versions.tf's required_version as dep type required_version of
// hashicorp/terraform, and a packageRule points it at opentofu/opentofu's releases (issue #422), so the pin
// moves with infra/tofu.yaml's tofuVersion and digests in one PR. Those release tags carry a v (v1.14.0) and
// required_version does not (1.14.0): the rule must strip it with extractVersion. Without it Renovate
// proposes v1.14.0, which dev/infra-check.sh refuses as unequal to infra/tofu.yaml's tofuVersion, so the PR
// it opens cannot be green. infra/tofu.yaml's own pins keep their v, which dev/tofu.sh and the release
// download need, so no rule may strip it there.
//
// The test sits beside test/lint/aireviewrenovate_test.go because that file already reads
// .github/renovate.json5 as the JSON5 document Renovate reads, with the tokenizer and value parser
// (json5Tokens, json5Value) reused here, and asks the same question of a different manager: which pinned text
// Renovate bumps, and in whose format. test/devenv/gatefiles_test.go reads the same file as text instead
// (stripJSON5Comments and one regexp per property), which cannot say which packageRule a property belongs to,
// so it is the wrong reader for a property that only some rules may set.
package lint_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// openTofuExtractVersion is the extractVersion Renovate must apply to OpenTofu's release tags before it
// compares them with infra/site/versions.tf's required_version: the group named version from a tag that
// starts with a v, so the proposal is in the pin's own format.
const openTofuExtractVersion = `^v(?<version>.+)$`

// One OpenTofu release as opentofu/opentofu tags it, and the version required_version pins: Renovate's
// github-releases datasource reports the tag, and extractVersion is what turns it into the proposal.
const (
	openTofuReleaseTag = "v1.14.0"
	openTofuVersion    = "1.14.0"
)

// TestOpenTofuRequiredVersionRuleStripsTheV pins that the packageRule covering the terraform manager's
// required_version dep type sets extractVersion, and that the value it sets strips the v from a release tag.
func TestOpenTofuRequiredVersionRuleStripsTheV(t *testing.T) {
	t.Parallel()

	rules, ok := renovateDocument(t, moduleRoot(t))["packageRules"].([]any)
	if !ok {
		t.Fatalf("%s: packageRules is no array, so this test cannot read the rule that covers infra/site/versions.tf's required_version (issue #434)", renovateConfigPath)
	}

	var extractVersion string

	selected := 0

	for i, entry := range rules {
		rule, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("%s: packageRules entry %d is no object", renovateConfigPath, i+1)
		}

		// The rule that owns the pin is the one that names both things the regexp matches: the manager that
		// reads the .tf file and the dep type the terraform manager gives required_version.
		if !renovateRuleSelects(rule, "matchManagers", "terraform") ||
			!renovateRuleSelects(rule, "matchDepTypes", "required_version") {
			continue
		}

		selected++

		// Renovate applies the rules in order, so the last one that sets a property is the one that holds.
		if value, ok := rule["extractVersion"].(string); ok {
			extractVersion = value
		}
	}

	if selected == 0 {
		t.Fatalf("%s: no packageRule selects the terraform manager's required_version dep type, so nothing moves infra/site/versions.tf's required_version with infra/tofu.yaml's tofuVersion (issue #422): the rule needs matchManagers: ['terraform'] and matchDepTypes: ['required_version']", renovateConfigPath)
	}

	if extractVersion != openTofuExtractVersion {
		t.Fatalf("%s: the packageRule covering the terraform manager's required_version sets extractVersion %q, want %q: OpenTofu's release tags carry a v (%s) and required_version does not (%s), so without this Renovate proposes %s and dev/infra-check.sh refuses the PR it opens (issue #434)",
			renovateConfigPath, extractVersion, openTofuExtractVersion, openTofuReleaseTag, openTofuVersion, openTofuReleaseTag)
	}

	// The value, read as the regexp it is. Renovate writes a named group the JavaScript way, (?<version>...),
	// and Go's regexp names it (?P<version>...): the two differ in that spelling alone.
	re, err := regexp.Compile(strings.ReplaceAll(extractVersion, "(?<", "(?P<"))
	if err != nil {
		t.Fatalf("%s: extractVersion %q is no regular expression: %v", renovateConfigPath, extractVersion, err)
	}

	match := re.FindStringSubmatch(openTofuReleaseTag)
	if match == nil {
		t.Fatalf("%s: extractVersion %q does not match the release tag %q", renovateConfigPath, extractVersion, openTofuReleaseTag)
	}

	got := ""

	for i, name := range re.SubexpNames() {
		if name == "version" {
			got = match[i]
		}
	}

	if got != openTofuVersion {
		t.Errorf("%s: extractVersion %q reads the release tag %q as version %q, want %q: Renovate would propose %q, which dev/infra-check.sh refuses beside infra/tofu.yaml's tofuVersion (issue #434)",
			renovateConfigPath, extractVersion, openTofuReleaseTag, got, openTofuVersion, got)
	}
}

// renovateDocument reads .github/renovate.json5 as the JSON5 document Renovate reads, through the tokenizer
// and value parser aireviewrenovate_test.go holds, and fails when the document does not read.
func renovateDocument(t *testing.T, root string) map[string]any {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, renovateConfigPath))
	if err != nil {
		t.Fatalf("read %s: %v; its packageRules decide which pins Renovate bumps (CLAUDE.md rule 7)", renovateConfigPath, err)
	}

	tokens, err := json5Tokens(string(src))
	if err != nil {
		t.Fatalf("%s: %v", renovateConfigPath, err)
	}

	value, next, err := json5Value(tokens, 0)
	if err != nil {
		t.Fatalf("%s: %v", renovateConfigPath, err)
	}

	if next != len(tokens) {
		t.Fatalf("%s: the document holds %d tokens after its one value", renovateConfigPath, len(tokens)-next)
	}

	document, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s: the document is no object", renovateConfigPath)
	}

	return document
}

// renovateRuleSelects reports whether a packageRule lists value among the selectors it holds under key
// (matchManagers, matchDepTypes): each is an array of strings, and a rule with no such key selects nothing.
func renovateRuleSelects(rule map[string]any, key, value string) bool {
	selectors, ok := rule[key].([]any)
	if !ok {
		return false
	}

	for _, selector := range selectors {
		if text, ok := selector.(string); ok && text == value {
			return true
		}
	}

	return false
}
