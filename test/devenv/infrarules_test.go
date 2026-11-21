// Pins the static rules of infra/'s OpenTofu configuration (issue #422): dev/infra-rules.sh reads the files
// under an infra root and reports, one stderr line "infra-rules: [<rule>] <file>[:<line>]: <why>" per broken
// rule, what tofu test cannot state - no API key resource, no ACL or visibility, exactly one IAM application
// and its one policy, only scaleway/scaleway, local modules only, one s3 backend and one required_version -
// equal to infra/tofu.yaml's tofuVersion - for every applied root, which no module source may reach and no
// configuration file may sit beside, no provider endpoint, every test file mocking the provider, and so on
// (the script lists them). With every rule holding it prints nothing and exits 0; a usage error exits 2.
//
// The real infra/ must hold every rule, and each case below is one copy of infra/ with exactly one breakage,
// built in t.TempDir() so the real tree stays untouched and the cases cannot drift apart: the copy is what
// infra/ holds today, whatever a later configuration adds. The case then pins the [<rule>] tags the script
// must report, and that it reports no other tag, so a breakage the script refuses for an unintended reason
// fails here instead of passing for the wrong reason. The cases need no network, no credentials and no
// credential file, and run no tofu: the script reads files only.
package devenv_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// infraRulesScript is the static rules' checker: make infra-check runs it on infra/ before any tofu
	// command, and the CI workflow runs it through dev/infra-check.sh.
	infraRulesScript = "dev/infra-rules.sh"
	// infraRulesRoot is the tree the rules must hold on: the repository's own infra/.
	infraRulesRoot = "infra"
	// infraRulesSiteDir is the site's configuration under an infra root, the directory every case breaks.
	infraRulesSiteDir = "site"
)

// infraRuleTag matches a rule's tag in the script's stderr line "infra-rules: [<rule>] ...".
var infraRuleTag = regexp.MustCompile(`(?m)^infra-rules: \[([a-z0-9-]+)\] `)

// infraRuleCase is one broken copy of infra/: what the case breaks, and the rule tags the script must report.
// A case with no tag breaks nothing a rule refuses, and its copy must pass.
type infraRuleCase struct {
	name     string            // the subtest's name
	why      string            // the breakage, for the reader
	want     string            // the rule tag the script must report, or "" when every rule still holds
	wantMore []string          // the further tags a breakage must report beside want, for the cases that pin a whole refusal
	wantIn   map[string]string // per tag, a substring the stderr line that reports it must hold: {root} and {site} stand for the copy's root and its site directory
	breakage func(t *testing.T, site string)
}

// infraRuleCases are the breakages dev/infra-rules.awk must refuse, one rule each. breakage gets the absolute
// path of the copied infra/site; the anchors it edits are strings of the real configuration, so a case fails
// loudly (as a test failure, never as a silent pass) when the configuration moves under it.
var infraRuleCases = []infraRuleCase{
	{
		name: "module-from-git",
		why:  `a module with source = "git::https://example.com/m"`,
		want: "module-source",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "m" {
  source = "git::https://example.com/m"
}
`)
		},
	},
	{
		name: "module-from-a-path-that-climbs-out-of-the-configuration",
		why:  `a module with source = "./../../../../tmp/x", a relative path whose .. segments leave the root`,
		want: "module-source",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "m" {
  source = "./../../../../tmp/x"
}
`)
		},
	},
	{
		name: "module-with-a-local-source",
		why:  `a module with source = "./m", a local module directory beside the configuration, which is the one source a rule allows`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "m" {
  source = "./m"
}
`)
			writeInfraText(t, filepath.Join(site, "m", "main.tf"), "variable \"v\" {}\n")
		},
	},
	{
		name: "object-visibility",
		why:  `a scaleway_object whose visibility = "public-read" would make the object an ACL`,
		want: "visibility",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object" "index" {
  bucket     = scaleway_object_bucket.site.name
  key        = "index.html"
  visibility = "public-read"
}
`)
		},
	},
	{
		name: "bucket-acl-argument",
		why:  `acl = "private" on the site bucket, an ACL even when it is private`,
		want: "acl",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"  region     = var.region\n\n  versioning {",
				"  region     = var.region\n  acl        = \"private\"\n\n  versioning {")
		},
	},
	{
		name: "bucket-acl-resource",
		why:  "a scaleway_object_bucket_acl resource beside the bucket policy",
		want: "bucket-acl",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object_bucket_acl" "site" {
  bucket = scaleway_object_bucket.site.name
}
`)
		},
	},
	{
		name: "acl-resource-type-written-with-an-escape",
		why:  `a resource whose type spells the underscore of scaleway_object_bucket_acl as \u005f, so the text no longer reads as an ACL`,
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object_bucket\u005facl" "x" {
}
`)
		},
	},
	{
		name: "api-key-resource",
		why:  "a scaleway_iam_api_key resource, whose secret would reach the state",
		want: "api-key",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_iam_api_key" "deploy" {
  application_id = scaleway_iam_application.deploy.id
}
`)
		},
	},
	{
		name: "api-key-resource-type-written-with-an-escape",
		why:  `a resource whose type spells the underscore of scaleway_iam_api_key as \u005f, so the text no longer reads as an API key`,
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_iam_api\u005fkey" "deploy" {
}
`)
		},
	},
	{
		name: "api-key-data-source",
		why:  `a data "scaleway_iam_api_key" block, whose attributes no rule reads`,
		want: "api-key",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
data "scaleway_iam_api_key" "k" {
}
`)
		},
	},
	{
		name: "second-iam-policy",
		why:  "a second scaleway_iam_policy at the deploy application's side",
		want: "iam-policy-count",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_iam_policy" "second" {
  name = "shift2eu-site-second"
}
`)
		},
	},
	{
		name: "second-iam-application",
		why:  "a second scaleway_iam_application",
		want: "iam-application-count",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_iam_application" "second" {
  name = "shift2eu-site-second"
}
`)
		},
	},
	{
		name: "iam-policy-repeated",
		why:  "count = 2 on the deploy policy, which makes it two policies",
		want: "iam-repeat",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"resource \"scaleway_iam_policy\" \"deploy\" {\n  name           = \"shift2eu-site-deploy\"",
				"resource \"scaleway_iam_policy\" \"deploy\" {\n  count          = 2\n  name           = \"shift2eu-site-deploy\"")
		},
	},
	{
		name: "iam-group",
		why:  "a scaleway_iam_group, the one IAM resource the site does not need",
		want: "iam-other",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_iam_group" "ops" {
  name = "shift2eu-site-ops"
}
`)
		},
	},
	{
		name: "iam-application-with-for-each",
		why:  "for_each on the deploy application, which makes it one application per element",
		want: "iam-repeat",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"resource \"scaleway_iam_application\" \"deploy\" {\n  name        = \"shift2eu-site-deploy\"",
				"resource \"scaleway_iam_application\" \"deploy\" {\n  for_each    = toset([\"shift2eu-site\"])\n  name        = \"shift2eu-site-deploy\"")
		},
	},
	{
		name: "second-provider-source",
		why:  `a required_providers entry aws = { source = "hashicorp/aws" }`,
		want: "provider-source",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				"  required_providers {\n    scaleway = {",
				"  required_providers {\n    aws = {\n      source = \"hashicorp/aws\"\n    }\n    scaleway = {")
		},
	},
	{
		name: "foreign-resource-type",
		why:  "a resource of another provider (terraform_data, then the built-in one)",
		want: "provider-type",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "terraform_data" "marker" {
  input = "the site"
}
`)
		},
	},
	{
		name: "provider-alias",
		why:  `alias = "real" on the configuration's provider block, a second provider instance under an unseen name`,
		want: "provider-alias",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "providers.tf"),
				"  organization_id = var.organization_id",
				"  alias           = \"real\"\n  organization_id = var.organization_id")
		},
	},
	{
		name: "provider-access-key",
		why:  `access_key = "x" in the configuration's provider block, a credential in a file git holds`,
		want: "provider-credentials",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "providers.tf"),
				"  organization_id = var.organization_id",
				"  access_key      = \"x\"\n  organization_id = var.organization_id")
		},
	},
	{
		name: "provider-profile",
		why:  `profile = "default" in the configuration's provider block, a credential source named in a file git holds`,
		want: "provider-credentials",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "providers.tf"),
				"  organization_id = var.organization_id",
				"  profile         = \"default\"\n  organization_id = var.organization_id")
		},
	},
	{
		name: "local-backend",
		why:  `backend "local" instead of the state bucket through backend "s3"`,
		want: "backend-s3",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				"backend \"s3\" {", "backend \"local\" {")
		},
	},
	{
		name: "second-backend",
		why:  "a second backend in the same root",
		want: "backend-count",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "versions.tf"), `
terraform {
  backend "s3" {
    key = "site/other.tfstate"
  }
}
`)
		},
	},
	{
		name: "cloud-block",
		why:  "a cloud block beside the backend",
		want: "cloud-block",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "versions.tf"), `
terraform {
  cloud {}
}
`)
		},
	},
	{
		name: "unpinned-required-version",
		why:  `required_version = ">= 1.13" instead of infra/tofu.yaml's exact release`,
		want: "required-version",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				`  required_version = "1.13.9"`, `  required_version = ">= 1.13"`)
		},
	},
	{
		name: "no-required-version",
		why:  `a root with no required_version at all, so any tofu release may read it`,
		want: "required-version",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				"  required_version = \"1.13.9\"\n\n", "")
		},
	},
	{
		name: "second-required-version",
		why:  "a second required_version in the same root, two constraints tofu would intersect",
		want: "required-version",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "versions.tf"), `
terraform {
  required_version = "1.13.9"
}
`)
		},
	},
	{
		name: "json-configuration",
		why:  "an x.tf.json file, which the rules do not read",
		want: "json-config",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "extra.tf.json"), "{}\n")
		},
	},
	{
		name: "override-file",
		why:  "an override.tf, which rewrites other files' blocks unseen",
		want: "override-file",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "override.tf"), "# An override file.\n")
		},
	},
	{
		name: "tofu-file-beside-a-tf-file-of-the-same-name",
		why:  "a versions.tofu beside versions.tf, a second file tofu loads under the same root",
		want: "shadowed-file",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "versions.tofu"), "# A .tofu twin of versions.tf.\n")
		},
	},
	{
		name: "tofutest-file-beside-a-tftest-file-of-the-same-name",
		why:  "an iam.tofutest.hcl beside iam.tftest.hcl, a second test file tofu test would also run",
		want: "shadowed-file",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "iam.tofutest.hcl"), `
mock_provider "scaleway" {}
`)
		},
	},
	{
		name: "heredoc",
		why:  "a heredoc, whose body the tokenizer does not read",
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  text = <<-EOT
    hello
  EOT
}
`)
		},
	},
	{
		name: "unclosed-string",
		why:  "a quoted string that does not end on its line",
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  text = "unterminated
}
`)
		},
	},
	{
		name: "unclosed-comment",
		why:  "a /* comment that never ends, which would swallow the rest of the file",
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "outputs.tf"), "\n/* the comment that never ends\n")
		},
	},
	{
		name: "symlink",
		why:  "a symlink under the root, whose target the rules do not read",
		want: "symlink",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			if err := os.Symlink(filepath.Join(site, "main.tf"), filepath.Join(site, "main_linked.tf")); err != nil {
				t.Fatalf("create the symlink: %v", err)
			}
		},
	},
	{
		name: "a-root-with-no-configuration",
		why:  "a root with no *.tf or *.tofu at all, so there is nothing the rules can read",
		want: "no-config",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			if err := os.RemoveAll(site); err != nil {
				t.Fatalf("remove the copied configuration: %v", err)
			}
		},
	},
	{
		name: "test-without-mock",
		why:  "a test file with no mock_provider \"scaleway\", so tofu test would run the real provider",
		want: "test-mock",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "unmocked.tftest.hcl"), "run \"plans\" {\n  command = plan\n}\n")
		},
	},
	{
		name: "test-with-a-provider",
		why:  "a provider block in a test file, which would configure the real provider",
		want: "test-provider",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), "\nprovider \"scaleway\" {}\n")
		},
	},
	{
		name: "aliased-mock-provider",
		why:  `a test file whose only mock_provider "scaleway" carries an alias, so no unaliased mock replaces the provider`,
		want: "test-mock",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "aliased.tftest.hcl"), `
mock_provider "scaleway" {
  alias = "real"
}
`)
		},
	},
	{
		name: "mock-provider-label-written-with-an-escape",
		why:  `a mock_provider whose label spells scaleway as scal\u0065way, so the real provider is no longer the one the file mocks`,
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "tests", "iam.tftest.hcl"),
				`mock_provider "scaleway" {`, `mock_provider "scal\u0065way" {`)
		},
	},
	{
		name: "test-run-block-with-providers",
		why:  `a run block with providers = { scaleway = scaleway.real }, which hands the run a real provider configuration`,
		want: "test-providers",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_providers" {
  command = plan

  providers = {
    scaleway = scaleway.real
  }
}
`)
		},
	},
	{
		name: "module-inside-a-test-run-block",
		why:  `a module block in a test file's run block, whose source is not a local ./ path`,
		want: "module-source",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_a_module" {
  command = plan

  module {
    source = "git::https://example.com/m"
  }
}
`)
		},
	},
	{
		name: "second-lock-file-provider",
		why:  "a second provider in .terraform.lock.hcl, so a foreign provider is installed",
		want: "lock-file",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, ".terraform.lock.hcl"), `
provider "registry.opentofu.org/hashicorp/aws" {
  version = "5.0.0"
  hashes  = ["h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="]
}
`)
		},
	},
	{
		name: "a-comment-naming-the-forbidden-things",
		why:  "a comment mentioning api_key and acl, which is no resource and no argument",
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
# The deploy api_key is made by hand, and no acl decides who reads the bucket: the bucket policy alone does.
`)
		},
	},
	// The applied root beside the configuration is never exempt: two routes reach it through a module source,
	// and a configuration file directly under the root points at it. Its backend and its required_version must
	// be judged whatever reaches it, a module may not point at an applied root, and no configuration file may
	// sit directly under the root at all.
	{
		name:     "test-file-module-source-pointing-at-a-root",
		why:      `a test file whose run block sets module source = "./site", the applied root beside tests/`,
		want:     "backend-count",
		wantMore: []string{"required-version"},
		wantIn:   map[string]string{"backend-count": "{site}", "required-version": "{site}"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			removeSiteBackendAndVersion(t, site)
			writeInfraText(t, filepath.Join(filepath.Dir(site), "tests", "x.tftest.hcl"), `
mock_provider "scaleway" {}

run "r" {
  module {
    source = "./site"
  }
}
`)
		},
	},
	rootFileModuleSourceCase("root-file-module-source-site", "./site"),
	rootFileModuleSourceCase("root-file-module-source-trailing-slash", "./site/"),
	rootFileModuleSourceCase("root-file-module-source-dot-segment", "./site/."),
	rootFileModuleSourceCase("root-file-module-source-leading-dot-segment", "././site"),
	// HCL decodes a template escape in a label, so the raw text is not the block name tofu reads.
	{
		name: "block-label-with-a-dollar-escape",
		why:  `a resource label written "a$${b}", which HCL reads as the literal "a${b}"`,
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object_bucket" "a$${b}" {
}
`)
		},
	},
	{
		name: "block-label-with-a-percent-escape",
		why:  `a resource label written "a%%{b}", which HCL reads as the literal "a%{b}"`,
		want: "parse",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object_bucket" "a%%{b}" {
}
`)
		},
	},
	{
		name: "provider-endpoint",
		why:  `api_url = "https://example.com" in the configuration's provider block, an endpoint named in a file git holds`,
		want: "provider-endpoint",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "providers.tf"),
				"  organization_id = var.organization_id",
				"  api_url         = \"https://example.com\"\n  organization_id = var.organization_id")
		},
	},
}

// rootFileModuleSourceCase is one route-2 case: a configuration file directly under the root, whose module
// source - written one way out of the spellings that reach the same directory - resolves to the applied root
// beside it. Every spelling must be refused, so none slips past the rules.
func rootFileModuleSourceCase(name, source string) infraRuleCase {
	return infraRuleCase{
		name:     name,
		why:      "a root.tf directly under the root whose module source " + source + " resolves to the applied root " + infraRulesSiteDir + "/ beside it",
		want:     "backend-count",
		wantMore: []string{"module-source", "required-version", "root-file"},
		wantIn: map[string]string{
			"backend-count":    "{site}",
			"required-version": "{site}",
			"module-source":    "{root}/root.tf",
			"root-file":        "{root}/root.tf",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			removeSiteBackendAndVersion(t, site)
			writeInfraText(t, filepath.Join(filepath.Dir(site), "root.tf"), `
terraform {
  required_version = "1.13.9"

  backend "s3" {
  }
}

module "m" {
  source = "`+source+`"
}
`)
		},
	}
}

// TestInfraRules pins the static rules' two verdicts (issue #422): the repository's own infra/ holds every
// rule, and a copy of it with one breakage each is refused with that breakage's tag and no other.
func TestInfraRules(t *testing.T) {
	t.Parallel()

	requireBashAndAwk(t)

	t.Run("the real infra holds every rule", func(t *testing.T) {
		t.Parallel()

		assertInfraRules(t, runInfraRules(t, infraRulesRoot), 0, nil, nil)
	})

	t.Run("no root is a usage error", func(t *testing.T) {
		t.Parallel()

		got := runInfraRules(t)

		if got.stdout != "" {
			t.Errorf("the script wrote %q on stdout: a usage error is reported on stderr", got.stdout)
		}

		if got.exitCode != 2 {
			t.Errorf("exit code with no root = %d, want 2: stderr was:\n%s", got.exitCode, got.stderr)
		}

		if !strings.Contains(got.stderr, "usage:") {
			t.Errorf("stderr with no root = %q, want the usage line:\n%s", got.stderr, got.stderr)
		}
	})

	for _, tc := range infraRuleCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := copyInfraTree(t)
			site := filepath.Join(root, infraRulesSiteDir)
			if tc.breakage != nil {
				tc.breakage(t, site)
			}

			var wantTags []string
			if tc.want != "" {
				wantTags = append(wantTags, tc.want)
			}

			wantTags = append(wantTags, tc.wantMore...)

			wantExit := 0
			if len(wantTags) > 0 {
				wantExit = 1
			}

			assertInfraRules(t, runInfraRules(t, root), wantExit, wantTags, infraRuleWantIn(t, tc.wantIn, root, site))
		})
	}
}

// infraRuleWantIn resolves the {root} and {site} placeholders of a case's wantIn, so a case can name the copy's
// own directories, which live under t.TempDir() and so differ from run to run.
func infraRuleWantIn(t *testing.T, wantIn map[string]string, root, site string) map[string]string {
	t.Helper()

	if len(wantIn) == 0 {
		return nil
	}

	resolved := make(map[string]string, len(wantIn))
	for tag, in := range wantIn {
		resolved[tag] = strings.NewReplacer("{root}", root, "{site}", site).Replace(in)
	}

	return resolved
}

// infraRuleRun is one run of the script: what it exited with, what it printed, and the rule tags its stderr
// lines report.
type infraRuleRun struct {
	exitCode int
	stdout   string
	stderr   string
	tags     []string
}

// runInfraRules runs the script from the repository root with args, as a maintainer or a CI job does, and
// reports what it printed. The tree it reads is the one args names; no tofu runs and no credential is read.
func runInfraRules(t *testing.T, args ...string) infraRuleRun {
	t.Helper()

	cmd := exec.Command("bash", append([]string{infraRulesScript}, args...)...)
	cmd.Dir = moduleRoot

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	var exitErr *exec.ExitError

	err := cmd.Run()
	if errors.As(err, &exitErr) {
		err = nil
	}

	if err != nil {
		t.Fatalf("run %s %s: %v", infraRulesScript, strings.Join(args, " "), err)
	}

	got := infraRuleRun{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr != nil {
		got.exitCode = exitErr.ExitCode()
	}

	for _, match := range infraRuleTag.FindAllStringSubmatch(got.stderr, -1) {
		got.tags = append(got.tags, match[1])
	}

	return got
}

// assertInfraRules fails unless the run exited with wantExit, wrote nothing on stdout, reported exactly
// wantTags, one stderr line each and in any order, printed nothing at all when wantExit is 0, and - for every
// tag in wantIn - reported it on a line that holds the substring, so a rule cannot refuse something without
// naming the directory or file it judges.
func assertInfraRules(t *testing.T, got infraRuleRun, wantExit int, wantTags []string, wantIn map[string]string) {
	t.Helper()

	if got.stdout != "" {
		t.Errorf("the script wrote %q on stdout: it prints nothing when every rule holds and reports on stderr", got.stdout)
	}

	if got.exitCode != wantExit {
		t.Errorf("exit code = %d, want %d: stderr was:\n%s", got.exitCode, wantExit, got.stderr)
	}

	want, reported := slices.Clone(wantTags), slices.Clone(got.tags)
	slices.Sort(want)
	slices.Sort(reported)

	if diff := cmp.Diff(want, reported); diff != "" {
		t.Errorf("the rule tags the script reported (-want +got):\n%s\nstderr was:\n%s", diff, got.stderr)
	}

	if wantExit == 0 {
		if got.stderr != "" {
			t.Errorf("the script exited 0 but reported:\n%s\nwant nothing: every rule holds", got.stderr)
		}

		return
	}

	lines := infraRuleLines(got.stderr)
	if len(lines) != len(wantTags) {
		t.Errorf("stderr holds %d lines for %d broken rules, want one line per broken rule:\n%s",
			len(lines), len(wantTags), got.stderr)
	}

	for _, line := range lines {
		if !strings.HasPrefix(line, "infra-rules: [") || !strings.Contains(line, "] ") {
			t.Errorf("stderr line %q is not \"infra-rules: [<rule>] <file>[:<line>]: <why>\"", line)
		}
	}

	for tag, in := range wantIn {
		found := false

		for _, line := range lines {
			if !strings.Contains(line, "["+tag+"] ") {
				continue
			}

			found = true

			if !strings.Contains(line, in) {
				t.Errorf("the [%s] line %q does not name %q: the rule must name the directory or file it refuses", tag, line, in)
			}
		}

		if !found {
			t.Errorf("no stderr line reports [%s], so nothing names %q", tag, in)
		}
	}
}

// infraRuleLines returns the non-empty lines of text.
func infraRuleLines(text string) []string {
	var lines []string

	for _, line := range strings.Split(text, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

// requireBashAndAwk skips the test where the script or its parser is missing: the rules are a bash script over
// a POSIX awk program, and no Go code can stand in for either.
func requireBashAndAwk(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"bash", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed: the static rules are a bash script and a POSIX awk program: %v", tool, err)
		}
	}
}

// copyInfraTree copies infra/ into a fresh directory - the site's configuration, its tests and its lock file -
// and returns that directory. .terraform is left out, as it is a tool's working directory and not a file the
// rules read. The real tree is never edited, and each case breaks only its own copy.
func copyInfraTree(t *testing.T) string {
	t.Helper()

	src := filepath.Join(moduleRoot, infraRulesRoot)
	root := filepath.Join(t.TempDir(), infraRulesRoot)

	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() && entry.Name() == ".terraform" {
			return fs.SkipDir
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dst := filepath.Join(root, rel)

		if entry.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}

		body, err := os.ReadFile(path) //nolint:gosec // G122: the walk reads a copy of infra/ under t.TempDir(), which no other process writes
		if err != nil {
			return err
		}

		//nolint:gosec // G703: dst is the walk's own path under t.TempDir(), never a caller's
		return os.WriteFile(dst, body, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", src, root, err)
	}

	return root
}

// editInfraFile replaces the one occurrence of old in path with new. old must occur exactly once, so a case
// breaks the file it names and not a second copy of the same text.
func editInfraFile(t *testing.T, path, old, new string) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if n := strings.Count(string(body), old); n != 1 {
		t.Fatalf("%s holds %q %d times, want exactly once: the case does not name one breakage", path, old, n)
	}

	writeInfraText(t, path, strings.Replace(string(body), old, new, 1))
}

// appendInfraText appends text to path.
func appendInfraText(t *testing.T, path, text string) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	writeInfraText(t, path, string(body)+text)
}

// writeInfraText writes text to path, creating the directory above it.
func writeInfraText(t *testing.T, path, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil { //nolint:gosec // G703: path is under the test's own temporary directory
		t.Fatalf("write %s: %v", path, err)
	}
}

// removeSiteBackendAndVersion takes the applied root's backend and its required_version out of a copy, so an
// applied root has neither unless a module source exempts it from the rules: the two anchors are the real
// site/versions.tf.
func removeSiteBackendAndVersion(t *testing.T, site string) {
	t.Helper()

	versions := filepath.Join(site, "versions.tf")
	editInfraFile(t, versions, "  required_version = \"1.13.9\"\n\n", "")
	dropInfraBlock(t, versions, "  backend \"s3\" {")
}

// dropInfraBlock removes the one block in path whose header line is exactly header, through the line that
// closes it, so a case can take a whole block away without restating its body: header is a string of the real
// configuration, and the block's closing line is the same indentation and a }.
func dropInfraBlock(t *testing.T, path, header string) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	lines := strings.Split(string(body), "\n")

	start := -1

	for i, line := range lines {
		if line != header {
			continue
		}

		if start >= 0 {
			t.Fatalf("%s holds the block header %q twice, want exactly once: the case does not name one breakage", path, header)
		}

		start = i
	}

	if start < 0 {
		t.Fatalf("%s holds no block header %q: the case does not name one breakage", path, header)
	}

	closing := header[:len(header)-len(strings.TrimLeft(header, " \t"))] + "}"
	end := -1

	for i := start + 1; i < len(lines); i++ {
		if lines[i] == closing {
			end = i

			break
		}
	}

	if end < 0 {
		t.Fatalf("%s has no %q line closing the block opened by %q: the case does not name one breakage", path, closing, header)
	}

	writeInfraText(t, path, strings.Join(append(lines[:start:start], lines[end+1:]...), "\n"))
}
