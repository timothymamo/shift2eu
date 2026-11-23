// Pins the release-tag skill's checks of the site's deploy (PRD #297 milestone 3, PR C2): the site environment
// the deploy job runs behind, the two Scaleway keys that live in it and nowhere else, the variables the deploy
// reads, the switch that decides whether a release deploys at all, and the post-release check that reports the
// release's four site jobs and reads a skipped deploy as the expected outcome while the switch is unset.
//
// The job names are tied to release.yml's own in test/architecture (TestReleaseTagSkillNamesTheReleaseJobs), so
// a rename cannot drift.
package docs_test

import (
	"os"
	"strings"
	"testing"
)

// The names and values the skill's site-deploy checks must carry (PRD #297 milestone 3, ADR 0042).
const (
	siteEnvironment = "site"

	// siteDeployJob, siteCheckJob, siteProgramsJob and siteSkippedJob are the names of the four release.yml
	// jobs the post-release check reports: the names the workflow pins, tied to them by test/architecture.
	siteDeployJob   = "Deploy the site"
	siteCheckJob    = "Check the live site"
	siteProgramsJob = "Site programs"
	siteSkippedJob  = "Site deploy skipped"

	// siteDeploySwitch is the repository variable that decides whether a release deploys the site at all.
	siteDeploySwitch = "SITE_DEPLOY_ENABLED"
)

// TestReleaseTagSkillChecksTheSiteEnvironment pins what the release-tag skill must check about the site's
// deploy (PRD #297 milestone 3, PR C2): step 2's environment checks extended to the site environment (a tag
// branch policy, the two Scaleway keys inside it and none at repository level, the bucket and region
// variables), the switch's read, and step 7's report of the four site jobs with a skipped deploy read as the
// expected outcome while the switch is unset.
func TestReleaseTagSkillChecksTheSiteEnvironment(t *testing.T) {
	t.Parallel()

	skill := readDoc(t, os.DirFS(repoRoot), releaseTagSkill)

	for _, want := range []struct {
		text string
		why  string
	}{
		{
			text: siteEnvironment,
			why:  "the deploy job runs behind the site environment, whose branch policy is what keeps a workflow on another ref away from the site's key",
		},
		{
			text: "custom_branch_policies",
			why:  "the environment's deployment branch policy is what the skill's existing environment checks read, and the site environment gets the same check",
		},
		{
			text: "protected_branches",
			why:  "a protected-branches policy would let a branch named like a tag deploy, so the skill checks the policy is the custom one",
		},
		{
			text: "tag v*.*.*",
			why:  "the site environment admits release tags alone: a branch policy of branch main, or of nothing, is a gap the skill must report",
		},
		{
			text: "SCW_ACCESS_KEY",
			why:  "the environment holds the deploy's access key, and the skill checks it is there",
		},
		{
			text: "SCW_SECRET_KEY",
			why:  "the environment holds the deploy's secret key, and the skill checks it is there",
		},
		{
			text: "repository level",
			why:  "either key at repository level would reach every workflow, so the skill's repository-level listing check must name them",
		},
		{
			text: "SITE_BUCKET",
			why:  "the deploy reads the bucket name from the environment's variables, so the skill checks the variable is set",
		},
		{
			text: "SITE_REGION",
			why:  "the deploy reads the bucket's region from the environment's variables, so the skill checks the variable is set",
		},
		{
			text: siteDeploySwitch,
			why:  "the switch decides whether a release deploys at all: a release that never set it is not set up yet, and one that set it wrong is a gap",
		},
		{
			text: siteProgramsJob,
			why:  "the job that built the deploy program and the live check is reported, so a failed build of either is a failed release",
		},
		{
			text: siteDeployJob,
			why:  "the job that deployed the site is reported, as the verify job's conclusion is",
		},
		{
			text: siteCheckJob,
			why:  "the job that checked the live site after the deploy is reported: a failed check is a failed release",
		},
		{
			text: siteSkippedJob,
			why:  "the job that says why the deploy did not run is reported, and its notice is what the maintainer reads",
		},
		{
			text: "expected",
			why:  "a deploy skipped while the switch is unset is the expected outcome, never a failure the release is held for",
		},
	} {
		if !strings.Contains(skill, want.text) {
			t.Errorf("%s holds no %q: %s (PRD #297 milestone 3)", releaseTagSkill, want.text, want.why)
		}
	}

	// The switch is read where the skip is decided: a skill that names the
	// variable only as a value to set could not tell a release that deploys from
	// one that does not.
	if at := strings.Index(skill, siteDeploySwitch); at >= 0 {
		if !strings.Contains(skill[at:], "unset") {
			t.Errorf("%s names %s without saying what a release does while it is unset: a release that sets it to nothing skips the deploy and its live check, which is no failure (PRD #297 milestone 3)\n%s",
				releaseTagSkill, siteDeploySwitch, skill[at:])
		}
	}
}
