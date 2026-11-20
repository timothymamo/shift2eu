package review

import (
	"slices"
	"strings"
)

// diffHeader starts every file section of a unified diff git writes.
const diffHeader = "diff --git "

// FilterDiff returns diff without the file sections the reviewer does not read
// (issue #324, ADR 0033), and the paths it left out, each once and sorted.
//
// The list is fixed here, and no pull request can widen it: any path ending
// ".golden" with a directory named exactly testdata among its parent
// directories. The generated bundle of the HTTP API's document, which the list
// named too, went with the API (issue #393, ADR 0037). Paths are compared as
// git writes them, case-sensitively and uncleaned, so a path with an empty, "."
// or ".." element is never left out. A section goes only when every path its
// "diff --git a/<old> b/<new>" header names is left out, so a rename or copy
// between a reviewed path and a left-out one is reviewed, and only when its
// extended header shows a regular file's content change (isRegularContentChange):
// a symbolic link, a submodule or a mode change is always reviewed, whatever its
// path (auditor L1). A header the filter
// cannot read with certainty (a path git quoted, or one holding " b/") keeps
// its section. A section starts at a line beginning "diff --git " and runs to
// the next: a hunk's lines begin with " ", "+", "-" or "\", so no line of a
// file's content starts a section. Text before the first section is kept, and
// every kept section is byte for byte as it was. FilterDiff depends on diff
// alone.
func FilterDiff(diff string) (kept string, excluded []string) {
	var (
		out     strings.Builder
		section strings.Builder
	)

	flush := func() {
		if paths, ok := leftOutPaths(section.String()); ok {
			excluded = append(excluded, paths...)
		} else {
			out.WriteString(section.String())
		}

		section.Reset()
	}

	for line := range strings.SplitAfterSeq(diff, "\n") {
		if strings.HasPrefix(line, diffHeader) {
			flush()
		}

		section.WriteString(line)
	}

	flush()

	slices.Sort(excluded)

	return out.String(), slices.Compact(excluded)
}

// leftOutPaths returns the two paths of a file section's header, old then new,
// and true when the reviewer leaves the section out: every path it names is
// left out and it changes a regular file's content alone (FilterDiff).
// ScanLeftOut decides by it too, so the scan reads exactly the sections the
// review does not.
func leftOutPaths(section string) ([]string, bool) {
	paths, ok := sectionPaths(section)
	if !ok || slices.ContainsFunc(paths, isReviewed) || !isRegularContentChange(section) {
		return nil, false
	}

	return paths, true
}

// sectionPaths returns the two paths of a file section's "diff --git a/<old>
// b/<new>" header, and false for text that is no section or a header it cannot
// read with certainty: a quoted path, a missing prefix, or a header in which
// " b/" occurs other than once.
func sectionPaths(section string) ([]string, bool) {
	header, _, _ := strings.Cut(section, "\n")

	rest, ok := strings.CutPrefix(header, diffHeader+"a/")
	if !ok || strings.Count(rest, " b/") != 1 {
		return nil, false
	}

	oldPath, newPath, _ := strings.Cut(rest, " b/")
	if oldPath == "" || newPath == "" {
		return nil, false
	}

	return []string{oldPath, newPath}, true
}

// The file modes git writes for a symbolic link and a submodule (a gitlink).
const (
	symlinkMode = "120000"
	gitlinkMode = "160000"
)

// isRegularContentChange reports whether a file section's extended header (its
// lines after "diff --git " and before the first "---", "+++", "@@" or "Binary
// files" line) shows a change to a regular file's content alone: no symbolic
// link or submodule ("new file mode", "deleted file mode" or an "index" line
// carrying mode 120000 or 160000) and no mode change ("old mode", "new mode").
// An "index" line not of the form "index <old>..<new> [mode]" leaves the header
// unclassifiable, and the section is kept.
// Only the extended header is read, never a hunk's content, which the pull
// request's author writes (auditor L1, issue #324).
func isRegularContentChange(section string) bool {
	lines := strings.Split(section, "\n")

	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "Binary files") {
			return true
		}

		if strings.HasPrefix(line, "old mode ") || strings.HasPrefix(line, "new mode ") {
			return false
		}

		mode := ""

		switch {
		case strings.HasPrefix(line, "new file mode "):
			mode = strings.TrimPrefix(line, "new file mode ")
		case strings.HasPrefix(line, "deleted file mode "):
			mode = strings.TrimPrefix(line, "deleted file mode ")
		case strings.HasPrefix(line, "index "):
			indexMode, ok := parseIndexLine(line)
			if !ok {
				return false
			}

			mode = indexMode
		}

		if mode == symlinkMode || mode == gitlinkMode {
			return false
		}
	}

	return true
}

// parseIndexLine reads an extended header's "index <old>..<new>" line,
// optionally followed by a mode, and returns that mode ("" when the line names
// none). It reports false for a line of any other form: such a header cannot be
// classified, so its section is kept and reviewed (issue #324).
func parseIndexLine(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || len(fields) > 3 || fields[0] != "index" {
		return "", false
	}

	before, after, found := strings.Cut(fields[1], "..")
	if !found || before == "" || after == "" {
		return "", false
	}

	if len(fields) == 3 {
		return fields[2], true
	}

	return "", true
}

// isReviewed reports whether the reviewer reads the file at path: every path
// but the golden files of a testdata directory.
func isReviewed(path string) bool {
	elements := strings.Split(path, "/")
	if slices.ContainsFunc(elements, func(e string) bool { return e == "" || e == "." || e == ".." }) {
		return true
	}

	parents := elements[:len(elements)-1]

	return !strings.HasSuffix(path, ".golden") || !slices.Contains(parents, "testdata")
}
