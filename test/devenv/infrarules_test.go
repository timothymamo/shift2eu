// Pins the static rules of infra/'s OpenTofu configuration (issue #422): dev/infra-rules.sh reads the files
// under an infra root and reports, one stderr line "infra-rules: [<rule>] <file>[:<line>]: <why>" per broken
// rule, what tofu test cannot state - no API key resource, no ACL or visibility, exactly one IAM application
// and its one policy, only scaleway/scaleway, local modules only, one s3 backend and one required_version -
// equal to infra/tofu.yaml's tofuVersion - for every applied root, which no module source may reach and no
// configuration file may sit beside, no provider endpoint, every test file mocking the provider, a top-level
// block of an allowed type only, a run or mock_provider label that is an identifier (so no label's / can shift
// the blocks beneath it out of the rule that judges them), a block label whose %, / or : never decides what
// the rules see, a provider block setting only organization_id and
// region, a lock file holding only the line shapes tofu init writes, and so on (the script lists them). With
// every rule holding it prints nothing and exits 0; a usage error exits 2.
//
// The real infra/ must hold every rule, and each case below is one copy of infra/ with exactly one breakage,
// built in t.TempDir() so the real tree stays untouched and the cases cannot drift apart: the copy is what
// infra/ holds today, whatever a later configuration adds. The case then pins the [<rule>] tags the script
// must report, and that it reports no other tag, so a breakage the script refuses for an unintended reason
// fails here instead of passing for the wrong reason. The cases need no network, no credentials and no
// credential file, and run no tofu: the script reads files only.
//
// TestInfraTracksNoLocalFiles pins the other half of the same rule (issue #422, ADR 0039): git carries no
// file the maintainer's own machine keeps under infra/ - no tfvars file, no backend.hcl, no state file and
// nothing from a .terraform directory - only the .example copies of the values it needs. It reads the
// repository's index and writes nothing.
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
	name      string            // the subtest's name
	why       string            // the breakage, for the reader
	want      string            // the rule tag the script must report, or "" when every rule still holds
	wantMore  []string          // the further tags a breakage must report beside want, for the cases that pin a whole refusal
	wantIn    map[string]string // per tag, a substring the stderr line that reports it must hold: {root} and {site} stand for the copy's root and its site directory
	wantNotIn map[string]string // per tag, a substring the stderr line that reports it must not hold: {root} and {site} as in wantIn
	breakage  func(t *testing.T, site string)
}

// infraRuleCases are the breakages dev/infra-rules.awk must refuse, one rule each. breakage gets the absolute
// path of the copied infra/site; the anchors it edits are strings of the real configuration, so a case fails
// loudly (as a test failure, never as a silent pass) when the configuration moves under it. The file-reading
// functions' cases come from infraFileFunctionCases, one per name the rules must refuse, so a name dropped
// from them fails here instead of passing unrefused.
var infraRuleCases = append(append(append(append(append(infraFileFunctionCases(), namedInfraRuleCases...), infraTestNestedBlockCases...), infraTestLabelCases...), infraLabelPathCases...), infraConfigurationLabelQuoteCases...)

// namedInfraRuleCases are the breakages written out one by one, in the order the script's rules are read.
var namedInfraRuleCases = []infraRuleCase{
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
		why:  `a scaleway_object whose visibility = "public-read" would make the object an ACL, beside the object the rule refuses on its own`,
		want: "visibility",
		// The visibility argument is the ACL rule; the scaleway_object holding it breaks the object rule too,
		// and neither refusal may hide the other.
		wantMore: []string{"object"},
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
	// The resource, data and ephemeral types are an allow list of the ones main.tf uses today: a type no rule
	// names on its own is [resource-type], so a new Scaleway service cannot enter the configuration unreviewed.
	// A type a specific rule names keeps that one label, one finding per type.
	{
		name:   "resource-type-data-source",
		why:    `a data "scaleway_secret_version" source, a type main.tf does not use`,
		want:   "resource-type",
		wantIn: map[string]string{"resource-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
data "scaleway_secret_version" "s" {
  secret_id = "x"
}
`)
		},
	},
	{
		name:   "resource-type-resource",
		why:    `a resource "scaleway_secret_version", a type main.tf does not use, whose data would reach the state`,
		want:   "resource-type",
		wantIn: map[string]string{"resource-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_secret_version" "s" {
  secret_id = "x"
  data      = "eA=="
}
`)
		},
	},
	{
		name:   "resource-type-rdb-user",
		why:    `a resource "scaleway_rdb_user", a managed database the site does not use`,
		want:   "resource-type",
		wantIn: map[string]string{"resource-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_rdb_user" "u" {
  instance_id = "x"
  name        = "u"
  password    = "p"
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
	// The provider "scaleway" block may set only what infra/site/providers.tf sets today: organization_id
	// and region. Anything else (a project_id, a nested block, ...) is [provider-setting], because a setting
	// there changes which organization or project the plan reaches, or what a nested block's own provider
	// does. An attribute a rule names on its own keeps that rule's tag and no [provider-setting] beside it
	// (access_key, secret_key and profile stay [provider-credentials], api_url [provider-endpoint], alias
	// [provider-alias]).
	{
		name:   "provider-project-id",
		why:    `project_id = "00000000-0000-0000-0000-000000000000" in the configuration's provider block, a default project every resource without one would land in`,
		want:   "provider-setting",
		wantIn: map[string]string{"provider-setting": "{site}/providers.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "providers.tf"),
				"  organization_id = var.organization_id\n  region          = var.region",
				"  organization_id = var.organization_id\n  region          = var.region\n  project_id      = \"00000000-0000-0000-0000-000000000000\"")
		},
	},
	{
		name:   "provider-nested-block",
		why:    "a some_block {} nested in the configuration's provider block, a setting the rules do not know",
		want:   "provider-setting",
		wantIn: map[string]string{"provider-setting": "{site}/providers.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "providers.tf"),
				"  organization_id = var.organization_id\n  region          = var.region",
				"  organization_id = var.organization_id\n  region          = var.region\n\n  some_block {\n  }")
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
	// The top-level terraform block is an allow list: required_version, required_providers, backend and cloud,
	// each judged by its own rule. Anything else is [terraform-setting], because a block there changes how the
	// plan runs on the admin machine: an encryption block's external key provider runs a local command at init,
	// plan and apply (OpenTofu 1.13.9 ships it), and its kms and openbao key providers send ambient credentials
	// to an endpoint the block names.
	{
		name:   "terraform-encryption-block",
		why:    `a terraform block's encryption block whose external key provider runs command = ["echo", "hi"] on the machine that plans`,
		want:   "terraform-setting",
		wantIn: map[string]string{"terraform-setting": "{site}/encryption.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "encryption.tf"), `
terraform {
  encryption {
    key_provider "external" "k" {
      command = ["echo", "hi"]
    }

    method "aes_gcm" "m" {
      keys = key_provider.external.k
    }

    state {
      method = method.aes_gcm.m
    }
  }
}
`)
		},
	},
	{
		name:   "terraform-provider-meta-block",
		why:    `a terraform block's provider_meta "scaleway" {} block`,
		want:   "terraform-setting",
		wantIn: map[string]string{"terraform-setting": "{site}/provider_meta.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "provider_meta.tf"), `
terraform {
  provider_meta "scaleway" {}
}
`)
		},
	},
	{
		name:   "terraform-experiments-attribute",
		why:    "experiments = [] in a terraform block, a setting that turns on a release's experimental features",
		want:   "terraform-setting",
		wantIn: map[string]string{"terraform-setting": "{site}/experiments.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "experiments.tf"), `
terraform {
  experiments = []
}
`)
		},
	},
	// Nothing imports a resource: the Runbook creates every resource fresh, so an import block would adopt
	// whatever object the id names into the state unasked (ADR 0039).
	{
		name:   "import-block",
		why:    `a top-level import block adopting the site bucket's object`,
		want:   "import-block",
		wantIn: map[string]string{"import-block": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
import {
  to = scaleway_object_bucket.site
  id = "nl-ams/example"
}
`)
		},
	},
	// The top-level block types are an allow list: a configuration file holds only terraform, provider,
	// resource, data, ephemeral, variable, locals, output, module and import, a test file only run,
	// mock_provider, variables and provider. Any other top-level block is [block-type], because a block no
	// rule knows is a block no rule reads: moved and removed rewrite the state unseen, check asserts outside
	// every test, override_resource and override_data replace what a test plans with, and a mock_provider in a
	// configuration file mocks a provider tofu test never reads there. A block type a rule names on its own, in
	// the file kinds that rule reads, keeps that rule's tag and no [block-type] beside it: a configuration's
	// import block stays [import-block], and a test file's provider block stays [test-provider]. An import block
	// in a test file is no such case, since the import rule reads configuration files only: it is a top-level
	// block the test allow list refuses, so it stays [block-type] alone. The blocks nested in a refused one (the
	// assert in a check block) are no findings of their own, since the allow list is the top level's.
	{
		name:   "moved-block",
		why:    "a top-level moved block, which rewrites the state of the resources it names unseen",
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
moved {
  from = scaleway_object_bucket.site
  to   = scaleway_object_bucket.other
}
`)
		},
	},
	{
		name:   "check-block",
		why:    `a top-level check "c" block and the assert block it holds, whose assertions tofu runs outside every test file`,
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
check "c" {
  assert {
    condition     = true
    error_message = "x"
  }
}
`)
		},
	},
	{
		name:   "removed-block",
		why:    "a top-level removed block with no provisioner, which drops the object it names from the state unseen",
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
removed {
  from = scaleway_object_bucket.gone
}
`)
		},
	},
	{
		name:   "mock-provider-in-a-configuration-file",
		why:    `a mock_provider "scaleway" block in a configuration file, where tofu test reads no mock`,
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), "\nmock_provider \"scaleway\" {}\n")
		},
	},
	{
		name:   "list-block",
		why:    `a top-level list "x" {} block in a configuration file of its own, a block type of a later OpenTofu release`,
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/zz-list.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "zz-list.tf"), `list "x" {}
`)
		},
	},
	{
		name:   "import-block-in-a-test-file",
		why:    `a top-level import block in a test file, beside the mock_provider and the run block the test allow list holds`,
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/tests/zz.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "zz.tftest.hcl"), `mock_provider "scaleway" {}

import {
  to = scaleway_object_bucket.site
  id = "fr-par/x"
}

run "x" {
  command = plan
}
`)
		},
	},
	{
		name:   "override-resource-in-a-test-file",
		why:    "a top-level override_resource block in a test file, which replaces the object the test plans with",
		want:   "block-type",
		wantIn: map[string]string{"block-type": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
override_resource {
  target = scaleway_object_bucket.site
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
	// A mock provider is the test file's own declaration: a source = ... makes it one the test file does not
	// hold, so what the file mocks is no longer what the file holds.
	{
		name:   "mock-provider-with-a-source",
		why:    `a mock_provider "scaleway" with source = "./tests/mocks", mocks read from outside the test file`,
		want:   "mock-source",
		wantIn: map[string]string{"mock-source": "{site}/tests/m.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "m.tftest.hcl"), `
mock_provider "scaleway" {
  source = "./tests/mocks"
}
`)
		},
	},
	// A .tofutest.hcl is a test file tofu test runs as it runs a .tftest.hcl one, so a source = ... there is
	// the same mock read from outside the file.
	{
		name:   "mock-provider-source-in-a-tofutest-file",
		why:    `a site/tests/m.tofutest.hcl whose mock_provider "scaleway" sets source = "./tests/mocks"`,
		want:   "mock-source",
		wantIn: map[string]string{"mock-source": "{site}/tests/m.tofutest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "m.tofutest.hcl"), `
mock_provider "scaleway" {
  source = "./tests/mocks"
}
`)
		},
	},
	// A mock data file is a second mock beside the test files, which no test file's mock_provider names: the
	// mocks belong in the test file that uses them.
	{
		name:   "mock-data-file",
		why:    `a site/tests/mocks/scaleway.tfmock.hcl holding mock_resource "scaleway_object_bucket" {}`,
		want:   "mock-file",
		wantIn: map[string]string{"mock-file": "{site}/tests/mocks/scaleway.tfmock.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "mocks", "scaleway.tfmock.hcl"), `
mock_resource "scaleway_object_bucket" {}
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
		name:   "lock-file-provider-without-a-space",
		why:    `a lock file block written provider"registry.opentofu.org/hashicorp/external", where HCL takes the { without the space`,
		want:   "lock-file",
		wantIn: map[string]string{"lock-file": "{site}/.terraform.lock.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, ".terraform.lock.hcl"), `
provider"registry.opentofu.org/hashicorp/external" {
  version = "2.3.4"
}
`)
		},
	},
	// The lock file holds only the line shapes tofu init writes: blank lines, # comment lines, the one
	// Scaleway provider header (any whitespace between its tokens), version = "...", constraints = "...",
	// hashes = [, a quoted hash string with an optional trailing comma, ] and }. Every other line is
	// [lock-file]: a provider header behind a comment, a second provider, a line init never writes.
	{
		name:   "lock-file-provider-after-a-comment",
		why:    `a block whose header line is /* x */ provider "registry.opentofu.org/hashicorp/external" {, a second provider behind a comment`,
		want:   "lock-file",
		wantIn: map[string]string{"lock-file": "{site}/.terraform.lock.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, ".terraform.lock.hcl"), `
/* x */ provider "registry.opentofu.org/hashicorp/external" {
  version = "2.3.4"
  hashes = [
    "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
  ]
}
`)
		},
	},
	{
		name: "lock-file-header-with-extra-spaces",
		why:  `the Scaleway header written provider "registry.opentofu.org/scaleway/scaleway"  { with two spaces before the brace, a shape init's own formatting may hold`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, ".terraform.lock.hcl"),
				`provider "registry.opentofu.org/scaleway/scaleway" {`,
				`provider "registry.opentofu.org/scaleway/scaleway"  {`)
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
	// OpenTofu ignores every configuration file whose basename starts with "." (configs.IsIgnoredFile), so
	// such a file is no part of what runs: the rules must refuse it and read no other rule into it.
	{
		name:     "hidden-versions-file",
		why:      "site/versions.tf renamed to site/.versions.tf, the hidden file beside tofu's view, which holds the root's backend and its required_version",
		want:     "hidden-file",
		wantMore: []string{"backend-count", "required-version"},
		wantIn: map[string]string{
			"hidden-file":      "{site}/.versions.tf",
			"backend-count":    "{site}",
			"required-version": "{site}",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()

			versions := filepath.Join(site, "versions.tf")

			body, err := os.ReadFile(versions)
			if err != nil {
				t.Fatalf("read %s: %v", versions, err)
			}

			writeInfraText(t, filepath.Join(site, ".versions.tf"), string(body))

			if err := os.Remove(versions); err != nil {
				t.Fatalf("remove %s: %v", versions, err)
			}
		},
	},
	{
		name:   "hidden-test-file",
		why:    "site/tests/.x.tftest.hcl, a test file tofu test ignores, so its mock_provider is no mock",
		want:   "hidden-file",
		wantIn: map[string]string{"hidden-file": "{site}/tests/.x.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", ".x.tftest.hcl"), `
mock_provider "scaleway" {}
`)
		},
	},
	{
		name:   "root-file-tofu",
		why:    "a <root>/x.tofu, a configuration file directly under the root",
		want:   "root-file",
		wantIn: map[string]string{"root-file": "{root}/x.tofu"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(filepath.Dir(site), "x.tofu"), "variable \"v\" {}\n")
		},
	},
	{
		name:   "root-file-tftest",
		why:    "a <root>/x.tftest.hcl, a test file directly under the root",
		want:   "root-file",
		wantIn: map[string]string{"root-file": "{root}/x.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(filepath.Dir(site), "x.tftest.hcl"), `mock_provider "scaleway" {}
`)
		},
	},
	// A module directory a local source names is no applied root, so it needs no backend and no
	// required_version - at any depth a configuration's module source reaches.
	{
		name: "module-of-a-module",
		why:  `site/main.tf's module "m" (source = "./m") naming its own module "sub" (source = "./sub"), neither module directory a root`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "m" {
  source = "./m"
}
`)
			writeInfraText(t, filepath.Join(site, "m", "main.tf"), `
module "sub" {
  source = "./sub"
}
`)
			writeInfraText(t, filepath.Join(site, "m", "sub", "main.tf"), "variable \"v\" {}\n")
		},
	},
	// A module source reads a directory whose segment starts with ".": tofu loads it, while these rules
	// read the tree with a find that prunes .terraform and no tool that hides a dot directory.
	{
		name:   "module-under-dot-terraform",
		why:    `a module with source = "./.terraform/m", whose module directory sits in .terraform, the directory the rules prune`,
		want:   "module-source",
		wantIn: map[string]string{"module-source": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "m" {
  source = "./.terraform/m"
}
`)
			writeInfraText(t, filepath.Join(site, ".terraform", "m", "main.tf"), "variable \"v\" {}\n")
		},
	},
	{
		name:   "module-under-a-hidden-directory",
		why:    `a module with source = "./.hidden/m", whose module directory starts with "."`,
		want:   "module-source",
		wantIn: map[string]string{"module-source": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "m" {
  source = "./.hidden/m"
}
`)
			writeInfraText(t, filepath.Join(site, ".hidden", "m", "main.tf"), "variable \"v\" {}\n")
		},
	},
	{
		name:   "test-run-block-module-under-dot-terraform",
		why:    `a run block in site/tests/iam.tftest.hcl with module source = "./.terraform/m"`,
		want:   "module-source",
		wantIn: map[string]string{"module-source": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_a_module_in_a_dot_directory" {
  command = plan

  module {
    source = "./.terraform/m"
  }
}
`)
		},
	},
	{
		name:   "test-run-block-module-under-a-hidden-directory",
		why:    `a run block in site/tests/iam.tftest.hcl with module source = "./.hidden/m"`,
		want:   "module-source",
		wantIn: map[string]string{"module-source": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_a_module_in_a_hidden_directory" {
  command = plan

  module {
    source = "./.hidden/m"
  }
}
`)
		},
	},
	// A provisioner or connection block runs a command, or reaches another machine, outside what the
	// configuration declares: it belongs in the Runbook, not in a file git holds.
	{
		name:   "provisioner-local-exec",
		why:    `a local-exec provisioner on a second bucket, which runs a command on whichever machine applies`,
		want:   "provisioner",
		wantIn: map[string]string{"provisioner": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object_bucket" "x" {
  name = "x"

  provisioner "local-exec" {
    command = "echo hi"
  }
}
`)
		},
	},
	{
		name:   "connection-block",
		why:    `a connection block in the site's project resource, the transport a provisioner would use`,
		want:   "provisioner",
		wantIn: map[string]string{"provisioner": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"  description = \"www.shift2eu.com: the site's bucket and its Edge Services pipeline only (infra/site).\"\n}",
				"  description = \"www.shift2eu.com: the site's bucket and its Edge Services pipeline only (infra/site).\"\n\n  connection {\n    type = \"ssh\"\n  }\n}")
		},
	},
	{
		name: "provisioner-in-removed",
		why:  `a provisioner in a top-level removed block, which runs when the object it names is destroyed`,
		want: "provisioner",
		// The removed block is no top-level type the allow list holds, and its provisioner is one no rule
		// allows: both are real findings, and neither refusal may hide the other.
		wantMore: []string{"block-type"},
		wantIn:   map[string]string{"provisioner": "{site}/main.tf", "block-type": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
removed {
  from = scaleway_object_bucket.gone

  provisioner "local-exec" {
    when    = destroy
    command = "echo hi"
  }
}
`)
		},
	},
	{
		name: "provisioner-in-a-test-file",
		why:  `a local-exec provisioner inside a run block of site/tests/iam.tftest.hcl, which runs on the machine that tests`,
		want: "provisioner",
		// A provisioner is no block a run block allows either: neither refusal may hide the other.
		wantMore: []string{"test-nested-block"},
		wantIn:   map[string]string{"provisioner": "{site}/tests/iam.tftest.hcl", "test-nested-block": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_a_provisioner" {
  command = plan

  provisioner "local-exec" {
    command = "echo hi"
  }
}
`)
		},
	},
	// OpenTofu loads every *.auto.tfvars (and *auto.tfvars.json, hidden ones too) without anyone naming it,
	// so a value git holds would decide what is applied. A plain terraform.tfvars is the maintainer's own,
	// kept on the admin machine, and no rule refuses one.
	{
		name:   "auto-tfvars",
		why:    "a site/zz.auto.tfvars, which tofu loads on every plan without being named",
		want:   "tfvars-file",
		wantIn: map[string]string{"tfvars-file": "{site}/zz.auto.tfvars"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "zz.auto.tfvars"), "public_read = true\n")
		},
	},
	{
		name:   "auto-tfvars-json",
		why:    "a site/zz.auto.tfvars.json, the JSON form tofu loads on every plan",
		want:   "tfvars-file",
		wantIn: map[string]string{"tfvars-file": "{site}/zz.auto.tfvars.json"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "zz.auto.tfvars.json"), "{\"public_read\": true}\n")
		},
	},
	{
		name:   "hidden-auto-tfvars",
		why:    "a site/.h.auto.tfvars, a hidden auto-tfvars file, which tofu still loads",
		want:   "tfvars-file",
		wantIn: map[string]string{"tfvars-file": "{site}/.h.auto.tfvars"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, ".h.auto.tfvars"), "public_read = true\n")
		},
	},
	{
		name: "terraform-tfvars-is-allowed",
		why:  "a site/terraform.tfvars, the maintainer's own values on the admin machine, which git ignores",
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "terraform.tfvars"), "public_read = true\n")
		},
	},
	// The committed backend "s3" block names no bucket, no region and no endpoint: those come from the
	// git-ignored backend.hcl. So it may set only what versions.tf sets today - the key, the lock file and
	// the four Scaleway skips - and any other attribute or nested block is refused.
	{
		name:   "backend-insecure",
		why:    "insecure = true in the committed backend block, which turns off its TLS verification",
		want:   "backend-setting",
		wantIn: map[string]string{"backend-setting": "{site}/versions.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				"    skip_s3_checksum            = true\n  }",
				"    skip_s3_checksum            = true\n    insecure                    = true\n  }")
		},
	},
	{
		name:   "backend-endpoints",
		why:    "endpoints = { s3 = \"https://example.com\" } in the committed backend block, an endpoint a file git holds would send the state to",
		want:   "backend-setting",
		wantIn: map[string]string{"backend-setting": "{site}/versions.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				"    skip_s3_checksum            = true\n  }",
				"    skip_s3_checksum            = true\n    endpoints = { s3 = \"https://example.com\" }\n  }")
		},
	},
	{
		name:   "backend-assume-role-block",
		why:    "an assume_role block in the committed backend block, a role ARN in a file git holds",
		want:   "backend-setting",
		wantIn: map[string]string{"backend-setting": "{site}/versions.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				"    skip_s3_checksum            = true\n  }",
				"    skip_s3_checksum            = true\n\n    assume_role {\n      role_arn = \"arn:aws:iam::123456789012:role/x\"\n    }\n  }")
		},
	},
	// tofu never uploads an object, and the plan must read no file from the machine that runs it: the
	// release deploys the objects, and a plan that reads a local file differs per machine (ADR 0039).
	{
		name:   "object",
		why:    "a scaleway_object resource, which tofu would upload: the release deploys the objects",
		want:   "object",
		wantIn: map[string]string{"object": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_object" "index" {
  bucket = scaleway_object_bucket.site.name
  key    = "index.html"
}
`)
		},
	},
	{
		name:   "file-function-file",
		why:    `a locals value calling file("/etc/hostname"), which reads the machine that runs the plan`,
		want:   "file-function",
		wantIn: map[string]string{"file-function": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  hostname = file("/etc/hostname")
}
`)
		},
	},
	{
		name:   "file-function-pathexpand",
		why:    `a locals value calling pathexpand("~/x"), which resolves against the home directory of whoever runs`,
		want:   "file-function",
		wantIn: map[string]string{"file-function": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  home = pathexpand("~/x")
}
`)
		},
	},
	{
		name:   "file-function-in-a-template",
		why:    `a file() call inside a "${...}" template, which the tokenizer reads as template text`,
		want:   "file-function",
		wantIn: map[string]string{"file-function": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  text = "a${file("/etc/hostname")}b"
}
`)
		},
	},
	{
		name:   "file-function-in-a-template-directive",
		why:    `a file() call inside a "%{ if ... }" template directive, which the tokenizer reads as template text too`,
		want:   "file-function",
		wantIn: map[string]string{"file-function": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  text = "%{ if file("/etc/hostname") != "" }y%{ endif }"
}
`)
		},
	},
	{
		name: "file-function-name-in-a-comment",
		why:  `a comment naming file("x") and pathexpand("~/x"), which reads no file`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
# No file is read here: file("x") and pathexpand("~/x") are words in this comment.
`)
		},
	},
	{
		name: "file-function-name-in-a-string",
		why:  `the literal string "file(x)", which calls no function`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  text = "file(x)"
}
`)
		},
	},
	{
		name: "file-function-templatestring",
		why:  `an output calling templatestring(local.t, {}), which renders a template string`,
		want: "file-function",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "templatestring.tf"), `
locals {
  t = "x"
}

output "o" {
  value = templatestring(local.t, {})
}
`)
		},
	},
	// A test file is read by the machine that runs tofu test too, so a file function there reads the same
	// local file and changes the same plan: it is refused exactly as in a configuration file.
	{
		name:   "file-function-in-a-test-file",
		why:    `a variables block in site/tests/iam.tftest.hcl setting organization_id = file("/etc/hostname")`,
		want:   "file-function",
		wantIn: map[string]string{"file-function": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
variables {
  organization_id = file("/etc/hostname")
}
`)
		},
	},
	// A .tofu file is a configuration file tofu reads as it reads a .tf one, so the rules judge it the same.
	{
		name:   "file-function-in-a-tofu-file",
		why:    `a site/locals.tofu whose locals value calls file("/etc/hostname")`,
		want:   "file-function",
		wantIn: map[string]string{"file-function": "{site}/locals.tofu"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "locals.tofu"), `
locals {
  x = file("/etc/hostname")
}
`)
		},
	},
	// A zero-byte file is a file tofu reads too: the rules judge it as they judge any other.
	{
		name:   "empty-root-file",
		why:    "an empty <root>/empty.tf, a configuration file directly under the root",
		want:   "root-file",
		wantIn: map[string]string{"root-file": "{root}/empty.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(filepath.Dir(site), "empty.tf"), "")
		},
	},
	{
		name:   "empty-test-file",
		why:    "an empty site/tests/empty.tftest.hcl, a test file that mocks no provider",
		want:   "test-mock",
		wantIn: map[string]string{"test-mock": "{site}/tests/empty.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "empty.tftest.hcl"), "")
		},
	},
	// Every name tofu reads, hidden or not, is refused for what it is: a JSON configuration, an override
	// file, a .tofu twin and a test file that breaks a rule are all refused as hidden, and judged no other.
	{
		name:   "hidden-json-config",
		why:    "a site/.x.tf.json, a hidden JSON configuration",
		want:   "hidden-file",
		wantIn: map[string]string{"hidden-file": "{site}/.x.tf.json"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, ".x.tf.json"), "{}\n")
		},
	},
	{
		name:   "hidden-override-file",
		why:    "a site/.override.tf, a hidden override file",
		want:   "hidden-file",
		wantIn: map[string]string{"hidden-file": "{site}/.override.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, ".override.tf"), "variable \"o\" {}\n")
		},
	},
	{
		name:   "hidden-tofu-twin",
		why:    "a site/.main.tofu, a hidden twin of main.tf",
		want:   "hidden-file",
		wantIn: map[string]string{"hidden-file": "{site}/.main.tofu"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, ".main.tofu"), "# A hidden twin of main.tf.\n")
		},
	},
	{
		name:   "hidden-test-file-that-breaks-a-rule",
		why:    `a site/tests/.y.tftest.hcl holding provider "scaleway" {}, a hidden test file that mocks nothing`,
		want:   "hidden-file",
		wantIn: map[string]string{"hidden-file": "{site}/tests/.y.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", ".y.tftest.hcl"), "provider \"scaleway\" {}\n")
		},
	},
	{
		name:   "root-file-bare-tf",
		why:    "a <root>/x.tf holding one variable, a configuration file directly under the root",
		want:   "root-file",
		wantIn: map[string]string{"root-file": "{root}/x.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(filepath.Dir(site), "x.tf"), "variable \"v\" {}\n")
		},
	},
	{
		name: "backup-file-is-ignored",
		why:  `a site/main.tf~ holding "resource {", which tofu never loads and no rule reads`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "main.tf~"), "resource {\n")
		},
	},
}

// infraTestNestedBlockCases pin the allow list of the blocks nested directly in a test file's run and
// mock_provider blocks: [block-type] reads the top level only, so a block inside a run block is invisible to
// it, and a run may hold only what tofu test reads there. The list is per parent, since a mock_provider holds
// mocks and a run holds everything else:
//   - inside run: override_resource, override_data, override_module, assert, variables, module and
//     plan_options, plus expect_failures, which tofu writes as an attribute and which the list names anyway
//     so a future block spelling of it is no surprise;
//   - inside mock_provider: mock_resource and mock_data.
//
// Anything else is [test-nested-block]. Only the direct children are judged: a block inside an allowed one
// (values in an override_resource, an assert's body) is none of this rule's. The run and mock_provider of the
// real infra/site/tests must pass, which the "the real infra holds every rule" case pins.
var infraTestNestedBlockCases = []infraRuleCase{
	{
		name:   "run-block-with-an-unlisted-nested-block",
		why:    `a foo block directly inside a run block of site/tests/iam.tftest.hcl, a nested type tofu test does not read there`,
		want:   "test-nested-block",
		wantIn: map[string]string{"test-nested-block": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_an_unlisted_block" {
  command = plan

  foo {
  }
}
`)
		},
	},
	{
		name:   "mock-provider-with-an-unlisted-nested-block",
		why:    `a foo block directly inside a mock_provider "scaleway" of site/tests/zz-nested.tftest.hcl, a nested type tofu test does not read there`,
		want:   "test-nested-block",
		wantIn: map[string]string{"test-nested-block": "{site}/tests/zz-nested.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "zz-nested.tftest.hcl"), `mock_provider "scaleway" {
  foo {
  }
}
`)
		},
	},
	// The pass cases guard the list against a rule that refuses what tofu test does read. They are copies of
	// infra/ under t.TempDir() that only dev/infra-rules.sh reads, never tofu: the blocks are the ones the
	// real test files use (assert, variables, mock_resource) and the ones tofu test reads beside them.
	{
		name: "allowed-nested-blocks",
		why:  `a site/tests/zz-allowed.tftest.hcl whose mock_provider holds mock_resource, mock_data, override_resource and override_data, and whose run holds variables, module, plan_options, override_resource, override_data, override_module and assert (expect_failures as the attribute tofu writes)`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			writeInfraText(t, filepath.Join(site, "tests", "zz-allowed.tftest.hcl"), `mock_provider "scaleway" {
  mock_resource "scaleway_account_project" {
    defaults = {
      id = "11111111-1111-1111-1111-111111111111"
    }
  }

  mock_data "scaleway_account_project" {
    defaults = {
      id = "11111111-1111-1111-1111-111111111111"
    }
  }

  override_resource {
    target = scaleway_account_project.site
    values = {}
  }

  override_data {
    target = scaleway_account_project.site
    values = {}
  }
}

run "the_allowed_nested_blocks" {
  command = plan

  variables {
    public_read = false
  }

  expect_failures = [var.organization_id]

  module {
    source = "./m"
  }

  plan_options {
    refresh = true
  }

  override_resource {
    target = scaleway_object_bucket.site
    values = {}
  }

  override_data {
    target = scaleway_object_bucket.site
    values = {}
  }

  override_module {
    target = module.m
    values = {}
  }

  assert {
    condition     = true
    error_message = "x"
  }
}
`)
		},
	},
	// The rule judges the direct children of run and mock_provider only: what an allowed block holds is that
	// block's business, so the values block of an override_resource is no [test-nested-block].
	{
		name: "block-nested-deeper-in-an-allowed-block",
		why:  `a values block inside an override_resource of site/tests/iam.tftest.hcl's run block, two levels below the run`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_a_deeper_block" {
  command = plan

  override_resource {
    target = scaleway_object_bucket.site

    values {
    }
  }
}
`)
		},
	},
	// expect_failures is an attribute in a run block today, which the allowed-nested-blocks case writes. The
	// allow list names it as a block anyway, so a run that writes it that way is not refused: this case pins
	// that, since no other case writes the block spelling.
	{
		name: "expect_failures-as-a-block",
		why:  `an expect_failures block directly in a run block of site/tests/iam.tftest.hcl, the spelling the allow list names beside the attribute tofu writes today`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a_run_with_expect_failures_as_a_block" {
  command = plan

  expect_failures {
    targets = [var.organization_id]
  }
}
`)
		},
	},
}

// infraTestLabelCases pin [test-label] (issue #434, the auditor's first finding): the rules build a block's
// path by joining its labels with /, so a run or mock_provider label that holds a / made the block after it
// look like the label's own child: `run "x/assert" { weird_b {} }` read weird_b as a block inside a path
// segment named assert instead of a direct child of the run, and [test-nested-block] never saw it (the same
// label hid a run-block module from [module-source]). So a run's or a mock_provider's label must be an
// identifier, [A-Za-z_][A-Za-z0-9_-]*, and a label that is not one is [test-label].
//
// A label with a / is refused twice, and the second refusal is the one that matters: [test-label] names the
// label, and the block it was hiding is reported by the rule it hid from - [test-nested-block] for the
// unlisted block inside the run or mock_provider, [module-source] for the module that climbs out of the
// configuration. Both tags are required, so a fix that only tags the label and leaves the path shifted
// fails here: the label's / must no longer decide which block the rules see. The identifier pass case pins
// the other side, so the rule does not refuse a label tofu test reads (its - and its digits are identifiers).
// The [test-label] message quotes the label as the source writes it, never as the escaped path element the
// rules judge the block by: `run "a/b"` is named `run "a/b"`, not `run "a%2Fb"`, and `run "a%2Fb"` is not
// escaped twice to `run "a%252Fb"` - an operator reading the message must find the label they wrote.
var infraTestLabelCases = []infraRuleCase{
	{
		name:     "run-label-with-a-slash-hides-its-nested-block",
		why:      `a run "x/assert" in site/tests/iam.tftest.hcl whose label holds a /, with a weird_b block directly inside the run, a nested type tofu test does not read there`,
		want:     "test-label",
		wantMore: []string{"test-nested-block"},
		wantIn: map[string]string{
			"test-label":        "{site}/tests/iam.tftest.hcl",
			"test-nested-block": "{site}/tests/iam.tftest.hcl",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "x/assert" {
  command = plan

  weird_b {
  }
}
`)
		},
	},
	{
		name:     "mock-provider-label-with-a-slash-hides-its-nested-block",
		why:      `a mock_provider "scaleway/mock_data" in site/tests/iam.tftest.hcl whose label holds a /, with a weird_h block directly inside it, a nested type tofu test does not read there`,
		want:     "test-label",
		wantMore: []string{"test-nested-block"},
		wantIn: map[string]string{
			"test-label":        "{site}/tests/iam.tftest.hcl",
			"test-nested-block": "{site}/tests/iam.tftest.hcl",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
mock_provider "scaleway/mock_data" {
  weird_h {
  }
}
`)
		},
	},
	{
		name: "run-label-with-a-slash-hides-its-module-source",
		why:  `a run "x/assert" in site/tests/iam.tftest.hcl whose module block has source = "../../evil", a path out of the configuration`,
		want: "test-label",
		wantMore: []string{
			"module-source",
		},
		wantIn: map[string]string{
			"test-label":    "{site}/tests/iam.tftest.hcl",
			"module-source": "{site}/tests/iam.tftest.hcl",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "x/assert" {
  command = plan

  module {
    source = "../../evil"
  }
}
`)
		},
	},
	{
		name:   "run-label-with-a-space",
		why:    `a run "a b" in site/tests/iam.tftest.hcl, whose label holds a space and so is no identifier`,
		want:   "test-label",
		wantIn: map[string]string{"test-label": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a b" {
  command = plan
}
`)
		},
	},
	{
		name:   "run-label-with-a-colon",
		why:    `a run "a:b" in site/tests/iam.tftest.hcl, whose label holds the : the path joins a label with, and so is no identifier`,
		want:   "test-label",
		wantIn: map[string]string{"test-label": "{site}/tests/iam.tftest.hcl"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a:b" {
  command = plan
}
`)
		},
	},
	{
		name:      "run-label-with-a-slash-is-quoted-as-written",
		why:       `a run "a/b" in site/tests/iam.tftest.hcl: [test-label] must quote the label the way the source writes it, never as the escaped path element the rules judge the block by`,
		want:      "test-label",
		wantIn:    map[string]string{"test-label": `run "a/b"`},
		wantNotIn: map[string]string{"test-label": "%2F"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a/b" {
  command = plan
}
`)
		},
	},
	{
		name:      "run-label-written-with-an-escape-is-quoted-as-written",
		why:       `a run "a%2Fb" in site/tests/iam.tftest.hcl whose label holds a %: [test-label] must quote it as written, so its % is never escaped a second time to %252F`,
		want:      "test-label",
		wantIn:    map[string]string{"test-label": `run "a%2Fb"`},
		wantNotIn: map[string]string{"test-label": "%252F"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a%2Fb" {
  command = plan
}
`)
		},
	},
	{
		name: "run-and-mock-provider-labels-that-are-identifiers",
		why:  `a run "a-run_2" in site/tests/iam.tftest.hcl whose label holds the - and the digits an identifier allows, which tofu test reads`,
		want: "",
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "a-run_2" {
  command = plan

  assert {
    condition     = true
    error_message = "x"
  }
}
`)
		},
	},
}

// infraLabelPathCases pin the other half of the fix for the auditor's first finding (issue #434): the same
// path shift hid blocks in configuration files, where no [test-label] can name the label that caused it. A
// label holding a / added a segment to the block's path, so the block stopped being the type or depth the
// rule reads it at and the rule skipped it while reporting nothing: `module "a/b"` was no [module-source],
// `resource "scaleway_iam_policy" "p/q"` with count = 2 no [iam-repeat], `provider "x/y"` with api_url no
// [provider-type] and no [provider-endpoint]. Each case below is that input on the real configuration, so the
// label's / must no longer decide which block the rules see. The last case pairs the shift with a run whose
// label also breaks [test-label]: both refusals are required, so tagging the label alone is no fix.
var infraLabelPathCases = []infraRuleCase{
	{
		name:   "module-label-with-a-slash-from-git",
		why:    `a module "a/b" in site/main.tf whose label holds a /, with source = "git::https://example.com/x", a source no rule allows`,
		want:   "module-source",
		wantIn: map[string]string{"module-source": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
module "a/b" {
  source = "git::https://example.com/x"
}
`)
		},
	},
	{
		name:   "iam-policy-label-with-a-slash-repeated",
		why:    `the deploy policy's label written "p/q", with count = 2, the repeat the label's / hid from the rule; the block is renamed rather than added, so the root still holds its one policy`,
		want:   "iam-repeat",
		wantIn: map[string]string{"iam-repeat": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"resource \"scaleway_iam_policy\" \"deploy\" {\n  name           = \"shift2eu-site-deploy\"",
				"resource \"scaleway_iam_policy\" \"p/q\" {\n  count          = 2\n  name           = \"shift2eu-site-deploy\"")
		},
	},
	{
		name:   "iam-policy-label-with-a-percent-escape-repeated",
		why:    `the deploy policy's label written "p%2Fq", with count = 2: the % is escaped like the / and the : the path joins a label with, so a label that reads like an escape is still one segment and the rule still judges the block`,
		want:   "iam-repeat",
		wantIn: map[string]string{"iam-repeat": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"resource \"scaleway_iam_policy\" \"deploy\" {\n  name           = \"shift2eu-site-deploy\"",
				"resource \"scaleway_iam_policy\" \"p%2Fq\" {\n  count          = 2\n  name           = \"shift2eu-site-deploy\"")
		},
	},
	{
		name:   "iam-policy-label-with-a-colon-repeated",
		why:    `the deploy policy's label written "p:q", with count = 2: the : is escaped like the / the path joins a label with, so the rule still judges the block`,
		want:   "iam-repeat",
		wantIn: map[string]string{"iam-repeat": "{site}/main.tf"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "main.tf"),
				"resource \"scaleway_iam_policy\" \"deploy\" {\n  name           = \"shift2eu-site-deploy\"",
				"resource \"scaleway_iam_policy\" \"p:q\" {\n  count          = 2\n  name           = \"shift2eu-site-deploy\"")
		},
	},
	{
		name:     "provider-label-with-a-slash-and-an-endpoint",
		why:      `site/providers.tf's provider block written "x/y", with api_url = "https://example.com" beside the settings it may set`,
		want:     "provider-type",
		wantMore: []string{"provider-endpoint"},
		wantIn: map[string]string{
			"provider-type":     "{site}/providers.tf",
			"provider-endpoint": "{site}/providers.tf",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()

			providers := filepath.Join(site, "providers.tf")
			editInfraFile(t, providers, `provider "scaleway" {`, `provider "x/y" {`)
			editInfraFile(t, providers,
				"  organization_id = var.organization_id",
				"  api_url         = \"https://example.com\"\n  organization_id = var.organization_id")
		},
	},
	{
		name:     "run-label-with-a-slash-and-providers",
		why:      `a run "x/y" in site/tests/iam.tftest.hcl whose label holds a /, with providers = { scaleway = scaleway.real }`,
		want:     "test-providers",
		wantMore: []string{"test-label"},
		wantIn: map[string]string{
			"test-providers": "{site}/tests/iam.tftest.hcl",
			"test-label":     "{site}/tests/iam.tftest.hcl",
		},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "tests", "iam.tftest.hcl"), `
run "x/y" {
  command = plan

  providers = {
    scaleway = scaleway.real
  }
}
`)
		},
	},
}

// infraConfigurationLabelQuoteCases pin the other half of the fix for the auditor's first finding (issue #434):
// the five refusals that quote a configuration block's label - [provider-type] for a resource, data or
// ephemeral block, [iam-other], [resource-type], [provider-type] for a provider block, and [backend-s3] - must
// quote the label as the source writes it, never the %2F, %3A or %25 the rules escape into the block's path so
// a label's / cannot shift the block. A message carrying the escape names a type, a provider or a backend that
// no file holds, so the reader looks for something the configuration does not contain.
var infraConfigurationLabelQuoteCases = []infraRuleCase{
	{
		name:      "resource-type-label-written-with-a-slash-is-quoted-as-written",
		why:       `a resource "scaleway_a/b" "x" in site/main.tf, a type whose / the rules escape into the block's path: [resource-type] must quote the type the way the source writes it`,
		want:      "resource-type",
		wantIn:    map[string]string{"resource-type": `"scaleway_a/b"`},
		wantNotIn: map[string]string{"resource-type": "%2F"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			appendInfraText(t, filepath.Join(site, "main.tf"), `
resource "scaleway_a/b" "x" {
}
`)
		},
	},
	{
		name:      "backend-label-written-with-a-slash-is-quoted-as-written",
		why:       `site/versions.tf's backend block written "s3/x", a label whose / the rules escape into the block's path: [backend-s3] must quote the label the way the source writes it`,
		want:      "backend-s3",
		wantIn:    map[string]string{"backend-s3": `"s3/x"`},
		wantNotIn: map[string]string{"backend-s3": "%2F"},
		breakage: func(t *testing.T, site string) {
			t.Helper()
			editInfraFile(t, filepath.Join(site, "versions.tf"),
				`backend "s3" {`, `backend "s3/x" {`)
		},
	},
}

// infraFileFunctions are the functions a call of which reads a file, or resolves a path, on the machine that
// runs tofu: the plan would then differ per machine and could carry a local file into the state. Every name
// here gets its own case below, and the rules may refuse no fewer.
var infraFileFunctions = []string{
	"file",
	"filebase64",
	"filebase64sha256",
	"filebase64sha512",
	"fileexists",
	"fileset",
	"templatefile",
	"templatestring",
	"filemd5",
	"filesha1",
	"filesha256",
	"filesha512",
	"pathexpand",
	"abspath",
}

// infraFileFunctionCases returns one case per file-reading function, each calling it the way the configuration
// would: fileset, templatefile and templatestring take a second argument, so they are called with one.
func infraFileFunctionCases() []infraRuleCase {
	cases := make([]infraRuleCase, 0, len(infraFileFunctions))

	for _, function := range infraFileFunctions {
		call := function + `("a")`
		if function == "fileset" || function == "templatefile" || function == "templatestring" {
			call = function + `("a", "b")`
		}

		cases = append(cases, infraRuleCase{
			name:   "file-function-call-" + function,
			why:    `a locals value calling ` + call + `, which reads the machine that runs the plan`,
			want:   "file-function",
			wantIn: map[string]string{"file-function": "{site}/main.tf"},
			breakage: func(t *testing.T, site string) {
				t.Helper()
				appendInfraText(t, filepath.Join(site, "main.tf"), `
locals {
  x = `+call+`
}
`)
			},
		})
	}

	return cases
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

		assertInfraRules(t, runInfraRules(t, infraRulesRoot), 0, nil, nil, nil)
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

			assertInfraRules(t, runInfraRules(t, root), wantExit, wantTags, infraRuleWantIn(t, tc.wantIn, root, site), infraRuleWantIn(t, tc.wantNotIn, root, site))
		})
	}
}

// infraLocalFileCases are listings of tracked paths, as `git ls-files -- infra` prints them, and the local
// files among them: a tfvars file, backend.hcl or a state file, which hold the maintainer's own IDs, bucket
// names or state, or anything under a .terraform directory, a tool's working directory. The .example copies
// hold none of that, so the repository commits them. The fake listings pin the check, so the real listing
// below cannot pass it for the wrong reason.
var infraLocalFileCases = []struct {
	name    string
	tracked []string
	want    []string
}{
	{
		name: "the-committed-files",
		tracked: []string{
			"infra/site/main.tf",
			"infra/site/versions.tf",
			"infra/site/.terraform.lock.hcl",
			"infra/site/terraform.tfvars.example",
			"infra/site/backend.hcl.example",
		},
		want: nil,
	},
	{
		name:    "a-tfvars-file",
		tracked: []string{"infra/site/terraform.tfvars"},
		want:    []string{"infra/site/terraform.tfvars"},
	},
	{
		name:    "an-auto-tfvars-file",
		tracked: []string{"infra/site/zz.auto.tfvars"},
		want:    []string{"infra/site/zz.auto.tfvars"},
	},
	{
		name:    "an-auto-tfvars-json-file",
		tracked: []string{"infra/site/zz.auto.tfvars.json"},
		want:    []string{"infra/site/zz.auto.tfvars.json"},
	},
	{
		name:    "a-tfvars-json-file",
		tracked: []string{"infra/site/terraform.tfvars.json"},
		want:    []string{"infra/site/terraform.tfvars.json"},
	},
	{
		name:    "a-backend-file",
		tracked: []string{"infra/site/backend.hcl"},
		want:    []string{"infra/site/backend.hcl"},
	},
	{
		name:    "a-state-file",
		tracked: []string{"infra/site/terraform.tfstate"},
		want:    []string{"infra/site/terraform.tfstate"},
	},
	{
		name:    "a-state-backup-file",
		tracked: []string{"infra/site/terraform.tfstate.backup"},
		want:    []string{"infra/site/terraform.tfstate.backup"},
	},
	{
		name:    "the-terraform-working-directory",
		tracked: []string{"infra/site/.terraform/providers/registry.opentofu.org/scaleway/scaleway/2.93.0/linux_amd64/terraform-provider-scaleway"},
		want:    []string{"infra/site/.terraform/providers/registry.opentofu.org/scaleway/scaleway/2.93.0/linux_amd64/terraform-provider-scaleway"},
	},
}

// TestInfraTracksNoLocalFiles pins that git carries no file the maintainer's own machine keeps (issue #422,
// ADR 0039): infra/ commits its configuration and the .example copies of the values it needs, never a tfvars
// file, never backend.hcl, never a state file and nothing from a .terraform directory. The fake listings pin
// the check itself, so the real one cannot pass for the wrong reason; the real one runs git ls-files and
// writes nothing.
func TestInfraTracksNoLocalFiles(t *testing.T) {
	t.Parallel()

	for _, tc := range infraLocalFileCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, infraLocalFiles(tc.tracked)); diff != "" {
				t.Errorf("the local files among the listing (-want +got):\n%s\nlisting was:\n%s", diff, strings.Join(tc.tracked, "\n"))
			}
		})
	}

	t.Run("the real infra tracks no local file", func(t *testing.T) {
		t.Parallel()

		tracked := gitTrackedInfraFiles(t)
		if len(tracked) == 0 {
			t.Fatalf("git tracks no file under %s/: the listing is empty, so this check would pass for the wrong reason", infraRulesRoot)
		}

		if diff := cmp.Diff([]string(nil), infraLocalFiles(tracked)); diff != "" {
			t.Errorf("git tracks local files under %s/, which stay on the maintainer's machine (-want +got):\n%s\nthe tracked files were:\n%s",
				infraRulesRoot, diff, strings.Join(tracked, "\n"))
		}
	})
}

// infraLocalFiles returns the tracked paths among tracked that the maintainer's machine holds and the
// repository must not: a tfvars file, backend.hcl, a state file or anything under a .terraform directory.
// A path ending in .example is the sanitised copy the repository commits, so it is none of those.
func infraLocalFiles(tracked []string) []string {
	var local []string

	for _, path := range tracked {
		name := path[strings.LastIndex(path, "/")+1:]
		if strings.HasSuffix(name, ".example") {
			continue
		}

		switch {
		case strings.Contains("/"+path, "/.terraform/"):
			local = append(local, path)
		case strings.HasSuffix(name, ".tfvars") || strings.HasSuffix(name, ".tfvars.json"):
			local = append(local, path)
		case name == "backend.hcl":
			local = append(local, path)
		case strings.Contains(name, ".tfstate"):
			local = append(local, path)
		}
	}

	return local
}

// gitTrackedInfraFiles returns the paths git tracks under infra/, slash separated, as the rules read the tree.
// It reads the repository's index and writes nothing; a checkout without git skips the caller.
func gitTrackedInfraFiles(t *testing.T) []string {
	t.Helper()

	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is not installed: the tracked files under %s/ are read from the repository's index: %v", infraRulesRoot, err)
	}

	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve %s: %v", moduleRoot, err)
	}

	// -z keeps a path with a space in it whole, as test/architecture and test/lint read the index too.
	out, err := exec.CommandContext(t.Context(), git, "-C", root, "ls-files", "-z", "--", infraRulesRoot).Output()
	if err != nil {
		t.Fatalf("git ls-files -- %s in %s: %v", infraRulesRoot, root, err)
	}

	var tracked []string

	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" {
			tracked = append(tracked, rel)
		}
	}

	return tracked
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
// naming the directory or file it judges. For every tag in wantNotIn, the lines that report it must not hold
// the substring: a message that quoted a value the way the rules escaped it internally would name something
// the source does not write.
func assertInfraRules(t *testing.T, got infraRuleRun, wantExit int, wantTags []string, wantIn, wantNotIn map[string]string) {
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
				t.Errorf("the [%s] line %q does not hold %q: a rule names the file, directory or value it refuses", tag, line, in)
			}
		}

		if !found {
			t.Errorf("no stderr line reports [%s], so nothing names %q", tag, in)
		}
	}

	for tag, not := range wantNotIn {
		for _, line := range lines {
			if strings.Contains(line, "["+tag+"] ") && strings.Contains(line, not) {
				t.Errorf("the [%s] line %q holds %q: a message names what the source writes, never a value the rules escaped to judge it", tag, line, not)
			}
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
