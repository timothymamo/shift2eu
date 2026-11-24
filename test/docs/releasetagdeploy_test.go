// Pins the release-tag skill's checks of the site's deploy (PRD #297 milestone 3, PR C2): the site environment
// the deploy job runs behind, the two Scaleway keys that live in it and nowhere else, the variables the deploy
// reads, the switch that decides whether a release deploys at all, and the post-release check that reports the
// release's four site jobs and reads a skipped deploy as the expected outcome while the switch is unset. Step 7's
// recovery is the maintainer's in every branch of it, the re-run inside the 30-day window included: the agent runs
// none of them and never touches the environment's key.
//
// The job names are tied to release.yml's own in test/architecture (TestReleaseTagSkillNamesTheReleaseJobs), so
// a rename cannot drift.
package docs_test

import (
	"os"
	"strconv"
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
// variables), what each of steps 2 and 7 says a switch that is unset means (step 2: the release skips the
// deploy and its live check; step 7: the two jobs are `skipped`, read as the expected outcome, and the skip
// notice's job `success`), and step 7's report of the four site jobs and its recovery, which the maintainer
// carries out in all three of its branches: the agent re-runs no job, holds no key and reports the recovery.
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

	// What a switch that is unset means is said in the two steps that act on
	// it: step 2, which reads the switch before the tag is cut, and step 7,
	// which reports the run's site jobs. Naming the variable is not enough, and
	// neither is saying "unset" once: a maintainer who reads it only where the
	// switch is read, and not where the run is reported, cannot tell an
	// expected skip from a failure the release is held for.
	for _, want := range []struct {
		step int
		text string
		why  string
	}{
		{
			step: 2,
			text: "while " + siteDeploySwitch + " is unset",
			why:  "step 2 decides what an unset switch means before the release: a missing site environment, secret or variable is a site not set up yet, not a gap to stop for",
		},
		{
			step: 2,
			text: "the release skips the deploy and its live check",
			why:  "step 2 says what an unset switch does: a release that never set it skips the deploy and its live check and is still a release",
		},
		{
			step: 7,
			text: "while " + siteDeploySwitch + " is unset",
			why:  "step 7 reports the run's site jobs, so it must say what the switch's value does to them",
		},
		{
			step: 7,
			text: siteDeployJob + " and " + siteCheckJob + " are expected skipped",
			why:  "step 7 reads the deploy and its live check as expected skipped while the switch is unset, never as a failure the release is held for",
		},
		{
			step: 7,
			text: siteSkippedJob + " must be success",
			why:  "the skip notice's job must have succeeded, so a run in which the notice never ran is not read as an expected skip",
		},
		{
			step: 7,
			text: "30-day re-run window",
			why:  "GitHub re-runs a run or its jobs only within 30 days of the initial run, so the skill states that window, never the artifacts' 90-day retention: a re-run past it cannot happen",
		},
		{
			step: 7,
			text: "the maintainer re-runs " + siteDeployJob,
			why:  "a re-run of Deploy the site puts the site environment's Scaleway key in scope for whatever the job runs, so within the window the maintainer is the one who re-runs it, never the agent reading the skill",
		},
		{
			step: 7,
			text: "re-runs " + siteCheckJob,
			why:  "re-running Deploy the site re-runs Check the live site after it, so the maintainer re-runs one job and gets both",
		},
		// Past the window GitHub re-runs nothing, and the recovery is the
		// maintainer's alone: the bucket's key is theirs, and no agent reads,
		// holds or uses it (CLAUDE.md, rule 3). The skill therefore says who
		// restores the pages, with whose key, and who cuts the other way out.
		{
			step: 7,
			text: "the maintainer restores the last good version of each page",
			why:  "past the window the recovery is the maintainer's, never the agent's: the skill names the maintainer as the one who restores the pages",
		},
		{
			step: 7,
			text: "from the bucket's object versions",
			why:  "the rollback is per page, from the bucket's object versions, which needs no release at all",
		},
		{
			step: 7,
			text: "with their own key",
			why:  "the key is the maintainer's: CLAUDE.md rule 3 keeps it from every agent, so the skill must not tell the agent to restore a page with a key of its own",
		},
		{
			step: 7,
			text: "R12",
			why:  "the per-page rollback is the runbook's R12, so the skill points the maintainer at the step that carries it out",
		},
		{
			step: 7,
			text: "cuts a new release with a Release-As: footer",
			why:  "the other recovery after the window is a new release the maintainer cuts: release-please reads the footer when there is no releasable commit to cut one from",
		},
		// Every one of the three recoveries is the maintainer's, the re-run
		// inside the window included: it is the one that hands the site
		// environment's key to a job, and an agent that started it would put a
		// key it must never touch in scope (CLAUDE.md, rule 3). The skill must
		// therefore say that the agent carries out none of them, and name the
		// two things it never does with the key at all.
		{
			step: 7,
			text: "You run none of these",
			why:  "none of the three recoveries is the agent's to start: the re-run, the page restore and the new release all put the maintainer's key in scope or are the maintainer's decision, so the skill rules out all three and not only the two after the window",
		},
		{
			step: 7,
			text: "never re-run a job",
			why:  "a re-run of Deploy the site or Check the live site runs the job with the site environment's key in scope, so the skill forbids the agent a re-run of any job outright",
		},
		{
			step: 7,
			text: "never hold, read or use any Scaleway key",
			why:  "CLAUDE.md rule 3: the agent neither reads, holds nor uses the deploy's key, and the skill says so where the recovery that needs it is named",
		},
	} {
		step := skillStep(t, skill, want.step)
		if !strings.Contains(skillStepPlain(step), want.text) {
			t.Errorf("%s's step %d does not say %q: %s (PRD #297 milestone 3)\n%s",
				releaseTagSkill, want.step, want.text, want.why, step)
		}
	}

	// The recovery after the window is never the agent's to carry out: the
	// bucket's key is the maintainer's, and an agent that read it would break
	// CLAUDE.md rule 3. The skill must not address the agent as the one holding
	// a key here, which is what "with your own key" reads as.
	if strings.Contains(skillStepPlain(skillStep(t, skill, 7)), "your own key") {
		t.Errorf("%s's step 7 restores the page with \"your own key\": the bucket's key is the maintainer's, so the recovery names them, never the agent reading the skill (CLAUDE.md, rule 3; PRD #297 milestone 3)", releaseTagSkill)
	}

	// Step 7 must not keep the form the auditor reported (PRD #297 milestone
	// 3): "within the 30-day re-run window, re-run `Deploy the site`" addresses
	// the agent, and a re-run puts the site environment's Scaleway key in scope
	// for whatever the job runs. The recovery names the maintainer instead, and
	// the closing must not rule out the two after-window options alone, as "You
	// do neither" does, while the re-run stays open to the agent.
	step7 := skillStepPlain(skillStep(t, skill, 7))

	for _, old := range []struct {
		text string
		why  string
	}{
		{
			text: ", re-run " + siteDeployJob,
			why:  "that clause tells the agent reading the skill to re-run the job, and a re-run runs Deploy the site with the site environment's Scaleway key in scope: the maintainer re-runs it, the agent never does (CLAUDE.md, rule 3)",
		},
		{
			text: "You do neither",
			why:  "that closing rules out the two after-window recoveries alone, leaving the re-run inside the window open to the agent: the skill must rule out all three, as \"You run none of these\" does",
		},
	} {
		if strings.Contains(step7, old.text) {
			t.Errorf("%s's step 7 still says %q: %s (PRD #297 milestone 3)", releaseTagSkill, old.text, old.why)
		}
	}

	if strings.Contains(skill, "90 days") {
		t.Errorf("%s names 90 days: the programs' artifacts are kept 30 days, GitHub's re-run window, and the skill's recovery must not promise a re-run past it (PRD #297 milestone 3)", releaseTagSkill)
	}
}

// skillStep returns step n of the release-tag skill's numbered list: the text
// from its "n." line to the line of the step after it. Every step is a
// top-level list item, so its marker starts a line of its own.
func skillStep(t *testing.T, skill string, n int) string {
	t.Helper()

	start := strings.Index(skill, "\n"+strconv.Itoa(n)+". ")
	if start < 0 {
		t.Fatalf("%s holds no step %d", releaseTagSkill, n)
	}

	start++

	next := strings.Index(skill[start:], "\n"+strconv.Itoa(n+1)+". ")
	if next < 0 {
		return skill[start:]
	}

	return skill[start : start+next]
}

// skillStepPlain returns a step with Markdown's backticks and emphasis removed
// and its whitespace collapsed, so a claim is matched wherever its sentence
// wraps and whichever emphasis the skill gives it.
func skillStepPlain(step string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("`", "", "*", "").Replace(step)), " ")
}
