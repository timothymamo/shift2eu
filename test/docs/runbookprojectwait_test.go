// Pins that the site runbook waits for a default-project switch to reach
// Scaleway's S3 before it runs the next tofu step (issue #537).
//
// The maintainer's own Scaleway key makes its S3 calls in the key's default
// project, and `scw iam api-key update "$SCW_ACCESS_KEY"
// default-project-id=...` takes a moment to apply at Scaleway. A tofu line run
// straight after the switch therefore reads the state in the wrong project:
// R7's step 8 switched back to the admin project and planned at once, and the
// plan left the state lock behind. Every line that switches the key's default
// project and is followed by a `dev/tofu.sh` line in the same section owes a
// short bounded wait between the two - a `#` comment saying why, then a listing
// of the state bucket that stops as soon as it answers. R7 owes one thing more:
// a way out of the lock a leftover one already left, `site/terraform.tfstate.tflock`
// named, and the `force-unlock` line carrying its PASS line.
//
// The check reads docs/site-runbook.md as the repository holds it, so it needs
// neither the aws CLI nor the scw CLI.
package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// projectWaitR7 is the section whose switch back to the admin project left
	// a state lock behind, and the one that owes the recovery lines.
	projectWaitR7 = "R7"

	// projectWaitSwitchCommand and projectWaitProjectFlag pick out a line that
	// switches the maintainer's own key to another project: the switch whose
	// effect at Scaleway the wait waits for.
	projectWaitSwitchCommand = "scw iam api-key update"
	projectWaitProjectFlag   = "default-project-id="

	// projectWaitTofuCommand starts the command a wait must come before.
	projectWaitTofuCommand = "dev/tofu.sh"

	// projectWaitLockFile is the lock a crashed or interrupted tofu leaves in
	// the state bucket, and projectWaitUnlockCommand the command that clears it.
	projectWaitLockFile      = "site/terraform.tfstate.tflock"
	projectWaitUnlockCommand = "dev/tofu.sh -chdir=infra/site force-unlock <lock ID>"

	// projectWaitPassMarker is what the runbook's commands print their expected
	// result as, in the line itself or the one above it.
	projectWaitPassMarker = "# PASS:"
)

// projectWaitWaitFragments are what the wait line between a switch and the next
// tofu line must hold: a state-bucket listing that answers at once, bounded to
// two minutes, which stops as soon as S3 answers from the new project. The
// bucket is the runbook's own placeholder for the state bucket, the one R1's
// listing lines use (`aws s3api list-objects-v2 --bucket <state-bucket>`).
var projectWaitWaitFragments = []string{
	"list-objects-v2",
	"--bucket <state-bucket>",
	"--max-items 1",
	"seq 1 12",
	"break",
	"sleep 10",
}

// projectWaitSectionHeading matches a section heading, `## R7. The deploy key's
// limits`, and keeps its R number.
var projectWaitSectionHeading = regexp.MustCompile(`^##\s+(R\d+)\.`)

// projectWaitSection is one `## R<n>.` section of the runbook: its name, the
// 1-based number of its heading line and the lines below that heading.
type projectWaitSection struct {
	name  string   // the heading's R number, "R7"
	start int      // the 1-based line number of the heading
	lines []string // the section's lines, the heading's own excluded
}

// number returns the 1-based line number of the section's i-th line.
func (s projectWaitSection) number(i int) int {
	return s.start + 1 + i
}

// span returns the 1-based numbers of the section's heading and last line.
func (s projectWaitSection) span() (first, last int) {
	return s.start, s.start + len(s.lines)
}

// waits reports whether a wait line lies strictly between the switch at index
// from and the tofu line at index to, with a `#` comment line directly above
// it: the pair R7's step 8 lacked.
func (s projectWaitSection) waits(from, to int) bool {
	for i := from + 1; i < to; i++ {
		if !projectWaitIsWait(s.lines[i]) {
			continue
		}
		if projectWaitIsComment(s.lines[i-1]) {
			return true
		}
	}

	return false
}

// carriesPass reports whether the line at index i carries the runbook's PASS
// marker itself, or a `#` comment line directly above or directly below it
// does: a recovery line may put its expected result either way.
func (s projectWaitSection) carriesPass(i int) bool {
	if strings.Contains(s.lines[i], projectWaitPassMarker) {
		return true
	}

	for _, j := range []int{i - 1, i + 1} {
		if j < 0 || j >= len(s.lines) {
			continue
		}
		if projectWaitIsComment(s.lines[j]) && strings.Contains(s.lines[j], projectWaitPassMarker) {
			return true
		}
	}

	return false
}

// projectWaitPair is a line that switches the key's default project and the
// first `dev/tofu.sh` line that follows it in the same section: the two lines
// the runbook must put a wait between.
type projectWaitPair struct {
	section  projectWaitSection
	switchAt int // the switch's index in section.lines
	tofuAt   int // the tofu line's index in section.lines
}

// TestRunbookWaitsAfterADefaultProjectSwitchBeforeTofu pins that every line of
// the site runbook that switches the maintainer's key's default project, and is
// followed by a `dev/tofu.sh` line in the same section, puts a wait between the
// two (issue #537). Today R7's step 8 and R13 fall short.
func TestRunbookWaitsAfterADefaultProjectSwitchBeforeTofu(t *testing.T) {
	t.Parallel()

	sections := projectWaitSections(strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n"))
	pairs := projectWaitSwitchTofuPairs(sections)

	t.Run("the runbook holds a switch followed by a tofu line", func(t *testing.T) {
		t.Parallel()

		if len(pairs) == 0 {
			t.Fatalf("%s holds no line that switches the key's default project (%q with %q) and is followed by a %q line in the same section, so this check would pass on nothing: point it at the switches and the tofu lines again (issue #537)",
				siteRunbookPath, projectWaitSwitchCommand, projectWaitProjectFlag, projectWaitTofuCommand)
		}
	})

	t.Run("every switch is followed by a wait before the next tofu line", func(t *testing.T) {
		t.Parallel()

		for _, pair := range pairs {
			if pair.section.waits(pair.switchAt, pair.tofuAt) {
				continue
			}

			t.Errorf("%s: section %s switches the key's default project at line %d (`%s`) and runs tofu at line %d (`%s`) with no wait between them: a switch takes a moment to apply at Scaleway, so the tofu line reads the state in the old project and can leave a state lock behind. Add a `#` comment saying why, then directly below it one line holding all of %v (issue #537)",
				siteRunbookPath, pair.section.name,
				pair.section.number(pair.switchAt), strings.TrimSpace(pair.section.lines[pair.switchAt]),
				pair.section.number(pair.tofuAt), strings.TrimSpace(pair.section.lines[pair.tofuAt]),
				projectWaitWaitFragments)
		}
	})
}

// TestRunbookR7RecoversFromALeftoverStateLock pins that R7, the section whose
// switch back to the admin project planned before the switch had applied, names
// the lock file such a run leaves and the command that clears it, with the PASS
// line that says the lock is gone (issue #537).
func TestRunbookR7RecoversFromALeftoverStateLock(t *testing.T) {
	t.Parallel()

	sections := projectWaitSections(strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n"))

	section, ok := projectWaitSectionNamed(sections, projectWaitR7)
	if !ok {
		t.Fatalf("%s holds no `## %s.` section, the one whose switch back to the admin project left a state lock behind (issue #537)",
			siteRunbookPath, projectWaitR7)
	}

	first, last := section.span()

	t.Run("R7 names the lock file an interrupted tofu leaves", func(t *testing.T) {
		t.Parallel()

		if len(projectWaitIndexesContaining(section, projectWaitLockFile)) == 0 {
			t.Errorf("%s: R7 (lines %d-%d) never names %s: the plan run straight after the switch back to the admin project left that lock behind, so the runbook must say where the leftover lock is and how to list it (issue #537)",
				siteRunbookPath, first, last, projectWaitLockFile)
		}
	})

	t.Run("R7 runs force-unlock with a PASS line", func(t *testing.T) {
		t.Parallel()

		unlocks := projectWaitIndexesContaining(section, projectWaitUnlockCommand)
		if len(unlocks) == 0 {
			t.Errorf("%s: R7 (lines %d-%d) never runs `%s`: a lock the switch left behind is cleared with it, once no tofu runs (issue #537)",
				siteRunbookPath, first, last, projectWaitUnlockCommand)

			return
		}

		for _, i := range unlocks {
			if section.carriesPass(i) {
				continue
			}

			t.Errorf("%s:%d runs `%s` without a %q line, in the line itself or directly beside it: the runbook says what a command prints, and the cleared lock is the PASS this one owes (issue #537)",
				siteRunbookPath, section.number(i), projectWaitUnlockCommand, projectWaitPassMarker)
		}
	})
}

// projectWaitSections splits the runbook's lines into its `## R<n>.` sections,
// each running from its heading to the next `##` heading, the next section's.
// A `# ` line does not end a section: in this runbook every one of them is a
// comment - a numbered step of R7's or a FAIL line - and not a heading.
func projectWaitSections(lines []string) []projectWaitSection {
	var sections []projectWaitSection

	var (
		name  string
		start int // the 0-based index of the open section's heading line
		open  bool
	)

	flush := func(end int) {
		if !open {
			return
		}

		sections = append(sections, projectWaitSection{name: name, start: start + 1, lines: lines[start+1 : end]})
		open = false
	}

	for i, line := range lines {
		switch {
		case projectWaitSectionHeading.MatchString(line):
			flush(i)

			name = projectWaitSectionHeading.FindStringSubmatch(line)[1]
			start, open = i, true
		case strings.HasPrefix(line, "## "):
			flush(i)
		}
	}

	flush(len(lines))

	return sections
}

// projectWaitSectionNamed returns the section the runbook calls name.
func projectWaitSectionNamed(sections []projectWaitSection, name string) (projectWaitSection, bool) {
	for _, section := range sections {
		if section.name == name {
			return section, true
		}
	}

	return projectWaitSection{}, false
}

// projectWaitSwitchTofuPairs returns every switch line and the first tofu line
// that follows it in its own section.
func projectWaitSwitchTofuPairs(sections []projectWaitSection) []projectWaitPair {
	var pairs []projectWaitPair

	for _, section := range sections {
		for i, line := range section.lines {
			if !projectWaitIsSwitch(line) {
				continue
			}

			for j := i + 1; j < len(section.lines); j++ {
				if projectWaitIsTofu(section.lines[j]) {
					pairs = append(pairs, projectWaitPair{section: section, switchAt: i, tofuAt: j})

					break
				}
			}
		}
	}

	return pairs
}

// projectWaitIndexesContaining returns the indexes of the section's lines that
// hold fragment.
func projectWaitIndexesContaining(section projectWaitSection, fragment string) []int {
	var indexes []int

	for i, line := range section.lines {
		if strings.Contains(line, fragment) {
			indexes = append(indexes, i)
		}
	}

	return indexes
}

// projectWaitIsComment reports whether the line is a comment line: the runbook's
// prose, a FAIL line or a PASS line, never a command.
func projectWaitIsComment(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "#")
}

// projectWaitIsSwitch reports whether the line runs the command that switches
// the maintainer's own key's default project.
func projectWaitIsSwitch(line string) bool {
	if projectWaitIsComment(line) {
		return false
	}

	return strings.Contains(line, projectWaitSwitchCommand) && strings.Contains(line, projectWaitProjectFlag)
}

// projectWaitIsTofu reports whether the line is a `dev/tofu.sh` command.
func projectWaitIsTofu(line string) bool {
	if projectWaitIsComment(line) {
		return false
	}

	return strings.HasPrefix(strings.TrimLeft(line, " \t"), projectWaitTofuCommand)
}

// projectWaitIsWait reports whether the line is the wait a switch owes: a
// state-bucket listing that stops as soon as S3 answers the new project, bounded.
func projectWaitIsWait(line string) bool {
	if projectWaitIsComment(line) {
		return false
	}

	for _, fragment := range projectWaitWaitFragments {
		if !strings.Contains(line, fragment) {
			return false
		}
	}

	return true
}
