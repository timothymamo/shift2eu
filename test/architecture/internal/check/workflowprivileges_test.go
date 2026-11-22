package check_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// workflowDoc renders a small workflow with the trigger lines below on:, a
// workflow-level permissions block ("" for none) and a job-level permissions
// block ("" for none), each as a caller writes it in .github/workflows.
func workflowDoc(trigger, workflowPermissions, jobPermissions string) string {
	var b strings.Builder

	b.WriteString("name: Example\n\non:\n")
	b.WriteString(trigger)
	b.WriteString("\n")
	b.WriteString(workflowPermissions)
	b.WriteString("jobs:\n")
	b.WriteString("  build:\n")
	b.WriteString("    name: Build\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString(jobPermissions)
	b.WriteString("    steps:\n")
	// A synthetic workflow that copies none of .github/workflows, so its pin
	// stays literal: the privilege check reads no pin, and a Renovate update of
	// a real workflow leaves this one as true as it was (issue #440).
	b.WriteString("      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n")
	b.WriteString("        with:\n")
	b.WriteString("          persist-credentials: false\n")
	b.WriteString("      - name: Build\n")
	b.WriteString("        run: make build\n")

	return b.String()
}

// workflowOn renders workflowDoc's document with on: written as on, its first
// line included, for the triggers that do not fit the block form: a bare
// on: push, a flow list.
func workflowOn(on, workflowPermissions, jobPermissions string) string {
	return strings.Replace(workflowDoc("  pull_request:\n", workflowPermissions, jobPermissions), "on:\n  pull_request:\n", on, 1)
}

// TestWorkflowPrivileges pins the audit's rule of issue #72: only
// .github/workflows/release.yml may hold what publishes to a registry, mints an
// OIDC identity or stores attestations, and only it may run on a tag. Every
// other workflow is refused a packages: write, id-token: write or
// attestations: write permission, at workflow or job level, a write-all, a
// trigger that runs on a tag (on: push: tags: and the events GitHub runs for a
// tag, create, release and workflow_run), and any workflow that leaves its
// permissions to the repository default by declaring none at the top level.
func TestWorkflowPrivileges(t *testing.T) {
	t.Parallel()

	const (
		ciName      = ".github/workflows/ci.yml"
		releaseName = ".github/workflows/release.yml"

		onBranches = "  push:\n    branches: [main]\n  pull_request:\n"
		onTags     = "  push:\n    tags: [\"v*.*.*\"]\n"

		workflowRead = "permissions:\n  contents: read\n"
		noPermission = "permissions: {}\n"
	)

	// releasePleaseWriting is the release-please workflow of issues #142 and
	// #157, with a packages: write through GITHUB_TOKEN added to its
	// release-please job, the only job it holds that writes at all: the tag and
	// relabel jobs' tokens are the Apps'.
	releasePleaseWriting := mutate(t, releasePleaseWorkflow,
		"    # GITHUB_TOKEN reads only: the App's token does the writing.\n    permissions:\n      contents: read\n",
		"    # GITHUB_TOKEN reads only: the App's token does the writing.\n    permissions:\n      contents: read\n      packages: write\n",
	)

	tests := []struct {
		name     string
		workflow string
		src      string
		// wantIn is a part of the message the check must return; "" means
		// none at all.
		wantIn string
	}{
		// The workflows the repository runs on every pull request ask for
		// nothing a release needs.
		{
			name:     "a ci.yml-like workflow",
			workflow: ciName,
			src:      workflowDoc(onBranches, workflowRead, ""),
		},
		{
			name:     "a security.yml-like workflow",
			workflow: ciName,
			src: workflowDoc(
				"  push:\n    branches: [main]\n  pull_request:\n  schedule:\n    - cron: \"0 6 * * 1\"\n",
				workflowRead,
				"    permissions:\n      contents: read\n      security-events: write\n",
			),
		},
		{
			name:     "packages: read",
			workflow: ciName,
			src:      workflowDoc(onBranches, workflowRead, "    permissions:\n      packages: read\n"),
		},
		{
			name:     "permissions: {}",
			workflow: ciName,
			src:      workflowDoc(onBranches, noPermission, ""),
		},
		{
			name:     "a workflow_dispatch trigger",
			workflow: ciName,
			src:      workflowDoc("  workflow_dispatch:\n  pull_request:\n", workflowRead, ""),
		},
		{
			// A job's own permissions: add nothing to what the workflow's
			// permissions: already say, so a top-level read and a job-level read
			// are accepted together.
			name:     "a job-level permissions next to the workflow's",
			workflow: ciName,
			src:      workflowDoc(onBranches, workflowRead, "    permissions:\n      packages: read\n      contents: read\n"),
		},
		{
			name:     "permissions: read-all",
			workflow: ciName,
			src:      workflowDoc(onBranches, "permissions: read-all\n", ""),
		},
		{
			name:     "an on: [pull_request] flow list",
			workflow: ciName,
			src:      workflowOn("on: [pull_request]\n", workflowRead, ""),
		},

		// release.yml is the one workflow that publishes: it holds all three
		// and runs on a tag.
		{
			name:     "release.yml",
			workflow: releaseName,
			src: workflowDoc(
				onTags,
				noPermission,
				"    permissions:\n      packages: write\n      id-token: write\n      attestations: write\n",
			),
		},

		// Every publishing privilege, at job level and at workflow level, in a
		// workflow that is not the release. The path the message names tells the
		// two levels apart: a job's permissions: comes after jobs: build:, the
		// workflow's before jobs:, on line 10 of workflowDoc's document.
		{name: "packages: write at job level", workflow: ciName, src: workflowDoc(onBranches, workflowRead, "    permissions:\n      packages: write\n"), wantIn: "jobs: build: permissions: packages is write; only .github/workflows/release.yml may publish"},
		{name: "id-token: write at job level", workflow: ciName, src: workflowDoc(onBranches, workflowRead, "    permissions:\n      id-token: write\n"), wantIn: "jobs: build: permissions: id-token is write; only"},
		{name: "attestations: write at job level", workflow: ciName, src: workflowDoc(onBranches, workflowRead, "    permissions:\n      attestations: write\n"), wantIn: "jobs: build: permissions: attestations is write; only"},
		{name: "packages: write at workflow level", workflow: ciName, src: workflowDoc(onBranches, "permissions:\n  contents: read\n  packages: write\n", ""), wantIn: "line 10: permissions: packages is write; only"},
		{name: "id-token: write at workflow level", workflow: ciName, src: workflowDoc(onBranches, "permissions:\n  contents: read\n  id-token: write\n", ""), wantIn: "line 10: permissions: id-token is write; only"},
		{name: "attestations: write at workflow level", workflow: ciName, src: workflowDoc(onBranches, "permissions:\n  contents: read\n  attestations: write\n", ""), wantIn: "line 10: permissions: attestations is write; only"},
		{name: "write-all at workflow level", workflow: ciName, src: workflowDoc(onBranches, "permissions: write-all\n", ""), wantIn: "line 8: permissions is write-all, which grants packages, id-token, attestations write"},
		{name: "write-all at job level", workflow: ciName, src: workflowDoc(onBranches, workflowRead, "    permissions: write-all\n"), wantIn: "line 14: jobs: build: permissions is write-all, which grants"},
		{
			name:     "two privileges at once",
			workflow: ciName,
			src:      workflowDoc(onBranches, workflowRead, "    permissions:\n      packages: write\n      id-token: write\n"),
			wantIn:   "jobs: build: permissions: packages is write; only",
		},
		{
			// Only .github/workflows/release.yml itself is exempt: a workflow
			// whose name merely starts with release is checked like any other.
			name:     "a workflow named like the release",
			workflow: ".github/workflows/release-notes.yml",
			src:      workflowDoc(onBranches, workflowRead, "    permissions:\n      packages: write\n"),
			wantIn:   "jobs: build: permissions: packages is write; only",
		},

		// The workflow of issue #142, release-please.yml, publishes nothing
		// itself: a push to main runs it, it holds no top-level permission, and
		// its jobs only read, contents and actions alike, because the token
		// that writes is the release-please App's, not GITHUB_TOKEN, and the tag
		// it creates is created by the release-tag App. So this check has
		// nothing to refuse in it, as it has in release.yml, and its name buys
		// it nothing: a workflow with its shape that writes through
		// GITHUB_TOKEN, under that name or any other, is refused like every
		// other workflow that is not release.yml.
		{name: "the release-please workflow", workflow: ".github/workflows/release-please.yml", src: releasePleaseWorkflow},
		{
			name:     "the release-please workflow writing packages",
			workflow: ".github/workflows/release-please.yml",
			src:      releasePleaseWriting,
			wantIn:   "jobs: release-please: permissions: packages is write; only .github/workflows/release.yml may publish",
		},
		{
			name:     "the release-please workflow writing packages under another name",
			workflow: ".github/workflows/release-please-copy.yml",
			src:      releasePleaseWriting,
			wantIn:   "jobs: release-please: permissions: packages is write; only .github/workflows/release.yml may publish",
		},

		// A tag trigger is what a release runs on, so no other workflow may
		// have one.
		{name: "a tags trigger", workflow: ciName, src: workflowDoc(onTags, workflowRead, ""), wantIn: "on: push: tags runs the workflow on a tag; only .github/workflows/release.yml may"},
		{
			name:     "a tags trigger next to a branch trigger",
			workflow: ciName,
			src:      workflowDoc("  push:\n    branches: [main]\n    tags: [\"v*\"]\n  pull_request:\n", workflowRead, ""),
			wantIn:   "on: push: tags runs the workflow on a tag; only",
		},
		{name: "a tags-ignore trigger", workflow: ciName, src: workflowDoc("  push:\n    tags-ignore: [\"v*\"]\n  pull_request:\n", workflowRead, ""), wantIn: "on: push: tags-ignore runs the workflow on a tag; only"},

		// Other events GitHub runs for a tag: a created tag or branch, a
		// published release, and a workflow run the tag started. Each is
		// refused in every form on: takes, since the reader holds no list of
		// the events that carry a tag.
		{name: "a create trigger", workflow: ciName, src: workflowDoc("  create:\n  pull_request:\n", workflowRead, ""), wantIn: "on: create"},
		{name: "a release trigger", workflow: ciName, src: workflowDoc("  release:\n    types: [published]\n  pull_request:\n", workflowRead, ""), wantIn: "on: release"},
		{name: "a release trigger with nothing below it", workflow: ciName, src: workflowDoc("  release:\n  pull_request:\n", workflowRead, ""), wantIn: "on: release"},
		{name: "a workflow_run trigger", workflow: ciName, src: workflowDoc("  workflow_run:\n    workflows: [CI]\n    types: [completed]\n  pull_request:\n", workflowRead, ""), wantIn: "on: workflow_run"},
		{name: "a bare on: create", workflow: ciName, src: workflowOn("on: create\n", workflowRead, ""), wantIn: "on: create"},
		{name: "a bare on: release", workflow: ciName, src: workflowOn("on: release\n", workflowRead, ""), wantIn: "on: release"},
		{name: "a bare on: workflow_run", workflow: ciName, src: workflowOn("on: workflow_run\n", workflowRead, ""), wantIn: "on: workflow_run"},
		{name: "a release in an on: flow list", workflow: ciName, src: workflowOn("on: [pull_request, release]\n", workflowRead, ""), wantIn: "on: [pull_request, release]"},
		{name: "a create in an on: flow list", workflow: ciName, src: workflowOn("on: [create, pull_request]\n", workflowRead, ""), wantIn: "on: [create, pull_request]"},
		{name: "a workflow_run in an on: flow list", workflow: ciName, src: workflowOn("on: [pull_request, workflow_run]\n", workflowRead, ""), wantIn: "on: [pull_request, workflow_run]"},

		// A workflow that declares no permissions at the top level takes the
		// repository's default, which may grant packages: write, so it must
		// say what it holds: a job's own permissions: is not the whole
		// workflow's answer.
		{name: "a workflow without a top-level permissions", workflow: ciName, src: workflowDoc(onBranches, "", ""), wantIn: "no top-level permissions"},
		{name: "a workflow whose jobs declare permissions but the top level does not", workflow: ciName, src: workflowDoc(onBranches, "", "    permissions:\n      contents: read\n"), wantIn: "no top-level permissions"},
		{name: "a workflow whose jobs declare publishing permissions and whose top level declares none", workflow: ciName, src: workflowDoc(onBranches, "", "    permissions:\n      packages: write\n"), wantIn: "no top-level permissions"},

		// The triggers and the flow values the reader cannot follow are refused
		// rather than guessed at, since either way the workflow may run on a tag.
		{name: "a bare on: push", workflow: ciName, src: workflowOn("on: push\n", workflowRead, ""), wantIn: "on: push runs on a push of any tag; only"},
		{name: "an on: [push] flow list", workflow: ciName, src: workflowOn("on: [push, pull_request]\n", workflowRead, ""), wantIn: "on: [push, pull_request] runs on a push of any tag; only"},
		{name: "an on: push: with nothing below it", workflow: ciName, src: workflowDoc("  push:\n  pull_request:\n", workflowRead, ""), wantIn: "on: push runs on a push of any tag; only"},
		{name: "an on: push with a value on its line", workflow: ciName, src: workflowDoc("  push: branches\n  pull_request:\n", workflowRead, ""), wantIn: `on: push is "branches", which this check does not read and which may run on a tag`},
		{name: "a workflow-level permissions flow mapping", workflow: ciName, src: workflowDoc(onBranches, "permissions: {contents: read}\n", ""), wantIn: `line 8: permissions is "{contents: read}", which this check does not read; write a block mapping`},
		{name: "a job-level permissions flow mapping", workflow: ciName, src: workflowDoc(onBranches, workflowRead, "    permissions: {packages: write}\n"), wantIn: `jobs: build: permissions is "{packages: write}", which this check does not read; write a block mapping`},
		{name: "an empty document", workflow: ciName, src: "", wantIn: "the workflow is empty, so this check cannot tell what it may do"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.WorkflowPrivileges(tc.workflow, []byte(tc.src))

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("WorkflowPrivileges(%q) = %q, want no message", tc.workflow, got)
				}

				return
			}

			if !strings.Contains(strings.Join(got, "\n"), tc.wantIn) {
				t.Errorf("WorkflowPrivileges(%q) = %q, want a message containing %q", tc.workflow, got, tc.wantIn)
			}
		})
	}
}
