package architecture_test

import (
	"os/exec"
	"strings"
	"testing"
)

// auditHostileCorpus is the committed corpus row 13 of the scheduled security
// audit runs the released CLI on (PRD #175 milestone 2, ADR 0040): its GitHub
// Actions workflow copies it into a container image that the released CLI then
// reads as hostile input.
const auditHostileCorpus = ".github/security-audit/testdata/hostile-inventories"

// auditRegularFile is the only mode an entry of the corpus may have in the git
// index: a regular file, not executable. A symlink is 120000 and a directory
// appears as every file under it.
const auditRegularFile = "100644"

// TestAuditHostileCorpusIsRegularFiles pins that every entry of the audit's
// hostile corpus is a regular file in the git index. The corpus is copied into
// the image row 13's containers read, and a committed link would make it depend
// on paths outside it and on the checkout's settings: what the image received
// would then be whatever the runner's checkout happened to put there, never the
// file the corpus holds. The test also refuses an empty corpus, so a directory
// that loses its files is not read as "nothing to check".
//
// The mode is read with git ls-files -s, from the index, never from the
// checkout's file system: the index is what a commit records.
func TestAuditHostileCorpusIsRegularFiles(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	out, err := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "-s", "--", auditHostileCorpus).Output()
	if err != nil {
		t.Fatalf("git ls-files -s -- %s: %v", auditHostileCorpus, err)
	}

	var entries int

	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}

		// An entry is "<mode> <object> <stage>\t<path>".
		meta, path, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)

		if !ok || len(fields) != 3 {
			t.Fatalf("read the index entry %q: want <mode> <object> <stage>\\t<path>", line)
		}

		entries++

		if mode := fields[0]; mode != auditRegularFile {
			t.Errorf("%s is mode %s in the git index, want %s: the audit's hostile corpus holds regular files alone, so nothing it holds depends on paths outside it or on the checkout's settings (ADR 0040)", path, mode, auditRegularFile)
		}
	}

	if entries == 0 {
		t.Fatalf("git ls-files -s -- %s lists no entry: the audit's hostile corpus is committed, and row 13 copies it into the container", auditHostileCorpus)
	}
}
