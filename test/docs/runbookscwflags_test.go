// Pins the flags the site runbook's throwaway container commands pass to
// `scw container container create` against a current scw CLI (PRD #297
// milestone 4): the image is passed with image=, the CPU limit with
// mvcpu-limit=, and the memory limit with memory-limit-bytes= carrying a unit
// (GB), never a bare byte count.
//
// The runbook's two probes - the digest pull go/no-go and the header probe -
// are commands the maintainer runs once by hand, so a flag scw no longer takes
// fails them at the prompt rather than in a test. The checks are on the lines
// alone, without scw or a Scaleway account: never a plan, an apply or a call.
//
// It also pins that the namespace the lines pass as namespace-id="$NS" is the
// bare UUID: the runbook reads container_namespace_id from the OpenTofu output,
// which carries the region as a prefix, and strips it (${NS##*/}) before any
// command uses it.
package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// scwContainerCreate is the command the runbook's probes run.
	scwContainerCreate = "scw container container create"

	// scwCreateHelp is the prose that tells the maintainer to check the flags
	// against scw's own help. Such a line names the command but runs nothing.
	scwCreateHelp = "--help"

	// minScwContainerCreateLines is how many create lines the runbook must keep:
	// the digest pull go/no-go and the header probe, and never fewer, so a
	// deleted command cannot make the flag check pass on nothing.
	minScwContainerCreateLines = 2

	// scwNamespaceOutput is the OpenTofu output the runbook reads the namespace
	// from.
	scwNamespaceOutput = "container_namespace_id"

	// scwNamespaceStrip is how the runbook strips the region prefix from that
	// output: everything through the last slash.
	scwNamespaceStrip = "${NS##*/}"
)

// scwMemoryLimit matches a create line's memory-limit-bytes= flag and the value
// it passes.
var scwMemoryLimit = regexp.MustCompile(`(?:^|\s)memory-limit-bytes=([^\s]+)`)

// scwContainerCreateLines returns the runbook's lines that run
// `scw container container create`, with their line numbers. A line that names
// the command only to point at its --help runs nothing and is not one.
func scwContainerCreateLines(t *testing.T, doc string) ([]int, []string) {
	t.Helper()

	var numbers []int

	var commands []string

	for i, line := range strings.Split(doc, "\n") {
		if !strings.Contains(line, scwContainerCreate) || strings.Contains(line, scwCreateHelp) {
			continue
		}

		numbers = append(numbers, i+1)
		commands = append(commands, line)
	}

	return numbers, commands
}

// TestRunbookScwContainerCreateFlags pins the create lines' flags: image=, not
// registry-image=; mvcpu-limit=, not cpu-limit=; memory-limit-bytes= with a GB
// unit; and the namespace passed as the bare UUID.
func TestRunbookScwContainerCreateFlags(t *testing.T) {
	t.Parallel()

	doc := readDoc(t, os.DirFS(repoRoot), siteRunbookPath)

	numbers, commands := scwContainerCreateLines(t, doc)

	if len(commands) < minScwContainerCreateLines {
		t.Fatalf("%s holds %d line(s) that run `%s`, at line(s) %v, and this check needs at least %d: the digest pull go/no-go and the header probe are the runbook's create commands, so one that went missing is not a pass (PRD #297 milestone 4)",
			siteRunbookPath, len(commands), scwContainerCreate, numbers, minScwContainerCreateLines)
	}

	for i, line := range commands {
		number := numbers[i]

		if strings.Contains(line, "registry-image=") {
			t.Errorf("%s:%d passes registry-image= to `%s`, want image=: a current scw CLI takes the image with image= (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, scwContainerCreate, line)
		}

		if !regexp.MustCompile(`(?:^|\s)image=`).MatchString(line) {
			t.Errorf("%s:%d runs `%s` without image=, want the image passed with image= (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, scwContainerCreate, line)
		}

		// mvcpu-limit= contains cpu-limit=, so only a cpu-limit= after a blank
		// is the flag scw no longer takes.
		if strings.Contains(line, " cpu-limit=") {
			t.Errorf("%s:%d passes cpu-limit= to `%s`, want mvcpu-limit=: a current scw CLI names the CPU limit mvcpu-limit= (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, scwContainerCreate, line)
		}

		if !strings.Contains(line, "mvcpu-limit=") {
			t.Errorf("%s:%d runs `%s` without mvcpu-limit=, want the CPU limit passed with mvcpu-limit= (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, scwContainerCreate, line)
		}

		memory := scwMemoryLimit.FindStringSubmatch(line)
		if memory == nil {
			t.Errorf("%s:%d runs `%s` without memory-limit-bytes=, want a memory limit with a unit (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, scwContainerCreate, line)

			continue
		}

		value := strings.TrimRight(memory[1], `"'`)
		if !strings.HasSuffix(value, "GB") {
			t.Errorf("%s:%d passes memory-limit-bytes=%s, want a value with a GB unit such as 0.25GB, never a bare byte count (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, value, line)
		}

		if !strings.Contains(line, `namespace-id="$NS"`) {
			t.Errorf("%s:%d runs `%s` without namespace-id=\"$NS\", want the namespace the runbook reads and strips (PRD #297 milestone 4):\n\t%s",
				siteRunbookPath, number, scwContainerCreate, line)
		}
	}
}

// TestRunbookStripsTheNamespacePrefix pins that the line that reads
// container_namespace_id into NS strips the region prefix, on that line or the
// next: the output carries "<region>/<uuid>", and every command after it passes
// NS as namespace-id=, which takes the bare UUID (PRD #297 milestone 4).
func TestRunbookStripsTheNamespacePrefix(t *testing.T) {
	t.Parallel()

	lines := strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n")

	number := -1

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "NS=") && strings.Contains(line, scwNamespaceOutput) {
			number = i

			break
		}
	}

	if number < 0 {
		t.Fatalf("%s holds no NS= line reading %s: the runbook reads the namespace from the OpenTofu output, and every scw command passes it as namespace-id=\"$NS\" (PRD #297 milestone 4)", siteRunbookPath, scwNamespaceOutput)
	}

	for i := number; i < len(lines) && i <= number+1; i++ {
		if strings.Contains(lines[i], scwNamespaceStrip) {
			return
		}
	}

	t.Errorf("%s:%d reads NS from %s but neither that line nor the next strips the region prefix (%s), so namespace-id=\"$NS\" passes \"<region>/<uuid>\" where scw takes the bare UUID (PRD #297 milestone 4):\n\t%s",
		siteRunbookPath, number+1, scwNamespaceOutput, scwNamespaceStrip, lines[number])
}
