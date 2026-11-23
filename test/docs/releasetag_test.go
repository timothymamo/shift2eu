// Pins the release-tag skill's post-release check for the site's bundle (PRD #297 milestone 3, PR C1): a release
// publishes shift2eu-site_vX.Y.Z.tar.gz beside the CLI's archives and checksums.txt, so the check's asset list
// names it, reports the conclusions of the release's "Site bundle" and "Site browser smoke test" jobs, and tells
// the maintainer never to re-run "Site bundle" once the release is published, because a re-run cascades into the
// jobs that upload and publish the draft. The two job names the skill names are tied to release.yml's own in
// test/architecture (TestReleaseTagSkillNamesTheReleaseJobs), so a rename cannot drift.
package docs_test

import (
	"os"
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
