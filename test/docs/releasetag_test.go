// Pins the release-tag skill's post-release check for the site's bundle (PRD #297 milestone 3, PR C1): a release
// publishes shift2eu-site_vX.Y.Z.tar.gz beside the CLI's archives and checksums.txt, so the check's asset list
// names it, reports the conclusions of the release's "Site bundle" and "Site browser smoke test" jobs, and tells
// the maintainer never to re-run "Site bundle" once the release is published, because a re-run cascades into the
// jobs that upload and publish the draft. The two job names the skill names are tied to release.yml's own in
// test/architecture (TestReleaseTagSkillNamesTheReleaseJobs), so a rename cannot drift.
package docs_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The release-tag skill, and what its post-release check must say about the site's bundle.
const (
	releaseTagSkill = ".claude/skills/release-tag/SKILL.md"

	// siteBundleAsset is the name of the release asset the site job builds: shift2eu-site_<tag>.tar.gz, with
	// the v of the tag (PRD #297 milestone 3).
	siteBundleAsset = "shift2eu-site_"

	// siteBundleJob and siteBrowserJob are the names of the two release.yml jobs whose conclusions the check
	// reports: the names the workflow pins, tied to them by test/architecture.
	siteBundleJob  = "Site bundle"
	siteBrowserJob = "Site browser smoke test"
)

// siteBundleNoRerunWindow is how much of the skill is read after the words "never re-run", so that the job the
// prohibition names is found whether the sentence runs on or wraps onto the next line.
const siteBundleNoRerunWindow = 200

// TestReleaseTagSkillChecksTheSiteBundle pins what the release-tag skill's post-release check must say about the
// site's bundle (PRD #297 milestone 3, PR C1): its asset list names the bundle and checksums.txt, it reports the
// two jobs that build and smoke-test the bundle, and it tells the maintainer never to re-run the bundle's job
// after the release is published.
func TestReleaseTagSkillChecksTheSiteBundle(t *testing.T) {
	t.Parallel()

	skill := readDoc(t, os.DirFS(repoRoot), releaseTagSkill)

	for _, want := range []struct {
		text string
		why  string
	}{
		{
			text: siteBundleAsset,
			why:  "a release publishes the site's bundle beside the CLI's archives, so a missing entry would let a release without it pass the check (PRD #297 milestone 3)",
		},
		{
			text: "checksums.txt",
			why:  "checksums.txt lists the bundle's digest beside the archives', and the release is checked against it",
		},
		{
			text: siteBundleJob,
			why:  "the job that built the bundle is reported, as the verify job's conclusion is",
		},
		{
			text: siteBrowserJob,
			why:  "the job that smoke-tested the bundle is reported, so a bundle whose smoke test failed is a failed release",
		},
		{
			text: "never re-run",
			why:  "a re-run of the bundle's job cascades into the jobs that upload to and publish the draft, so the check must forbid it in so many words",
		},
	} {
		if !strings.Contains(skill, want.text) {
			t.Errorf("%s holds no %q: %s (PRD #297 milestone 3)", releaseTagSkill, want.text, want.why)
		}
	}

	// The prohibition names the job it forbids: a sentence that says "never re-run" of nothing in particular is
	// no instruction a maintainer can follow.
	at := strings.Index(skill, "never re-run")
	if at < 0 {
		return
	}

	if !strings.Contains(skill[at:min(at+siteBundleNoRerunWindow, len(skill))], siteBundleJob) {
		t.Errorf("%s's text after %q does not name %s: the recovery text says which job is never re-run, since a re-run of the bundle's job cascades into goreleaser and publish (PRD #297 milestone 3)\n%s",
			releaseTagSkill, "never re-run", siteBundleJob, skill[at:min(at+siteBundleNoRerunWindow, len(skill))])
	}
}

// The security-audit environment, its two listings and the names each must give (PRD #175 milestone 2,
// ADR 0040): the skill's secrets check lists the environment's secrets and variables the way it lists the
// other Apps', so that what the scheduled audit holds and what no other workflow may read are read by one
// check.
const (
	auditEnvironment = "security-audit"

	// auditHiddenListing and auditVariablesListing are the two reads the check must make: GitHub returns the
	// environment's secrets (their names, never their values) and its variables. The skill's prose wraps, so
	// they are matched with its whitespace collapsed.
	auditHiddenListing    = "gh api repos/{owner}/{repo}/environments/security-audit/secrets --jq '.secrets[].name'"
	auditVariablesListing = "gh api repos/{owner}/{repo}/environments/security-audit/variables --jq '.variables[].name'"

	// auditPolicyRead and auditPolicyListing are the reads of the environment itself and of its branch
	// policy, the way the AI reviewer's block reads ai-review's: the environment for the policy it applies,
	// and the one policy it lists. The read of the environment is followed by the words "must show", so it
	// is no prefix of the secrets and variables listings beside it.
	auditPolicyRead    = "gh api repos/{owner}/{repo}/environments/security-audit must show"
	auditPolicyListing = "gh api repos/{owner}/{repo}/environments/security-audit/deployment-branch-policies --jq '.branch_policies[] | .type + \" \" + .name'"
)

// auditHiddenNames and auditVariableNames are the names the environment must hold: the audit App's key
// and client ID, the two encryption key pairs' halves, the fingerprint key and the Claude Code token the
// model step runs on. GitHub keeps the first list's as secrets, whose values it never returns, and the
// second's as variables.
var (
	auditHiddenNames = []string{
		"AUDIT_APP_PRIVATE_KEY",
		"AUDIT_MODEL_PRIVATE_KEY",
		"AUDIT_PUBLISH_PRIVATE_KEY",
		"AUDIT_FINGERPRINT_KEY",
		"CLAUDE_CODE_OAUTH_TOKEN",
	}

	auditVariableNames = []string{
		"AUDIT_APP_CLIENT_ID",
		"AUDIT_MODEL_PUBLIC_KEY",
		"AUDIT_PUBLISH_PUBLIC_KEY",
	}

	// auditConfigurationName matches a name of the audit's own configuration in the skill's text: every
	// one starts with AUDIT_, and the environment's block must name exactly these.
	auditConfigurationName = regexp.MustCompile(`\bAUDIT_[A-Z0-9_]+\b`)

	// boldLeadIn matches the line a block of the skill's checks begins with bold text, the way the check
	// of the site's deploy and of the AI reviewer's secrets begin: the audit's block ends where the next
	// one starts.
	boldLeadIn = regexp.MustCompile(`\n[ \t]*\*\*`)
)

// auditConfigurationNames are the names auditConfigurationName finds among the environment's: the audit
// App's key and client ID, the model and publish key pairs' halves, and the fingerprint key.
// CLAUDE_CODE_OAUTH_TOKEN is the AI reviewer's name too and starts with no AUDIT_, so it is checked by
// name alone.
var auditConfigurationNames = []string{
	"AUDIT_APP_PRIVATE_KEY",
	"AUDIT_APP_CLIENT_ID",
	"AUDIT_MODEL_PRIVATE_KEY",
	"AUDIT_MODEL_PUBLIC_KEY",
	"AUDIT_PUBLISH_PRIVATE_KEY",
	"AUDIT_PUBLISH_PUBLIC_KEY",
	"AUDIT_FINGERPRINT_KEY",
}

// TestReleaseTagSkillChecksTheSecurityAuditEnvironment pins what the release-tag skill's secrets check must
// say about the scheduled audit's environment (PRD #175 milestone 2, ADR 0040): the two reads it makes, the
// names the environment holds, the branch policy that keeps a workflow on another ref away from the audit's
// keys and the protection rules that keep its runs unattended, and the rule that neither the audit App's key
// nor any other of the audit's secrets sits at repository level, where every workflow could read it. The
// names are exactly the environment's: a variable listed as a secret, a renamed name or a stray one would
// leave what the audit needs, or what it may not have beside another workflow, unchecked.
func TestReleaseTagSkillChecksTheSecurityAuditEnvironment(t *testing.T) {
	t.Parallel()

	skill := readDoc(t, os.DirFS(repoRoot), releaseTagSkill)
	block := skillStepPlain(auditEnvironmentBlock(t, skillStep(t, skill, 2)))

	for _, want := range []struct {
		text string
		why  string
	}{
		{
			text: auditHiddenListing,
			why:  "the environment's secrets are read the way the other Apps' are, so a misspelt path cannot pass for a check",
		},
		{
			text: auditVariablesListing,
			why:  "the environment's variables are read too: the audit App's client ID and the two key pairs' public halves are variables, not secrets",
		},
		{
			text: "actions/secrets",
			why:  "no secret of the audit's may sit at repository level, where every workflow reads it, so the repository-level secrets listing is checked against the names",
		},
		{
			text: "repository level",
			why:  "the check says where the audit's secrets may not sit, as the checks of the release Apps' and of the site's keys do",
		},
		{
			text: auditPolicyRead,
			why:  "the environment is read for its branch policy, as ai-review's is: anything but a custom policy on main lets a workflow on another ref read the audit's keys",
		},
		{
			text: "deployment_branch_policy.custom_branch_policies",
			why:  "the policy must be the custom one, whose list is read next: a protected-branches policy admits every branch and tag that GitHub protects, not main alone",
		},
		{
			text: "protected_branches",
			why:  "the check names the other policy by name, so a reader can tell which of the two it read, as the AI reviewer's block does",
		},
		{
			text: auditPolicyListing,
			why:  "the environment's branch policies are listed by the command ai-review's block uses, so a misspelt path cannot pass for a check",
		},
		{
			text: "branch main",
			why:  "the one policy the environment admits is main: a policy of a branch that may run the audit is a key a workflow on another branch can reach",
		},
		{
			text: "protection_rules",
			why:  "the environment's protection rules are read too: a required reviewer would hold every scheduled run until a human approves it, and the audit is unattended",
		},
		{
			text: "no required_reviewers",
			why:  "the rules must hold no reviewer at all, which is the claim the check makes of them",
		},
	} {
		if !strings.Contains(block, want.text) {
			t.Errorf("%s's secrets check holds no %q: %s (PRD #175 milestone 2)\n%s", releaseTagSkill, want.text, want.why, block)
		}
	}

	for _, name := range append(slices.Clone(auditHiddenNames), auditVariableNames...) {
		if !strings.Contains(block, name) {
			t.Errorf("%s's secrets check holds no %q: the %s environment holds it, and a maintainer who reads the check must see whether it is there (PRD #175 milestone 2)\n%s",
				releaseTagSkill, name, auditEnvironment, block)
		}
	}

	// The block names exactly the environment's AUDIT_ names: one listed in the other listing, a renamed
	// one, or one the environment does not hold, fails here.
	found := slices.Compact(slices.Sorted(slices.Values(auditConfigurationName.FindAllString(block, -1))))
	want := slices.Sorted(slices.Values(auditConfigurationNames))

	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s's secrets check names the audit's configuration names other than the environment's (PRD #175 milestone 2)\nwant:\n%s\ngot:\n%s",
			releaseTagSkill, strings.Join(want, "\n"), strings.Join(found, "\n"))
	}
}

// auditEnvironmentBlock returns the part of the release-tag skill's step 2 that checks the security-audit
// environment: the text from the environment's first mention to the next block of checks, the one the skill
// leads with bold text, or to the end of the step. A claim its neighbouring blocks make - the AI reviewer's,
// the site's - is no part of it.
func auditEnvironmentBlock(t *testing.T, step string) string {
	t.Helper()

	start := strings.Index(step, auditEnvironment)
	if start < 0 {
		t.Fatalf("%s's step 2 holds no %q: the secrets check must list the scheduled audit's environment as it lists the other Apps' (PRD #175 milestone 2)",
			releaseTagSkill, auditEnvironment)
	}

	run := step[start:]

	if end := boldLeadIn.FindStringIndex(run); end != nil {
		return run[:end[0]]
	}

	return run
}
