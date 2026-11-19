package review

import (
	"slices"
	"strings"
)

// generatedBundle is the one generated file the reviewer leaves out by its
// exact path: the OpenAPI bundle generated from api/ (issue #324, ADR 0033).
const generatedBundle = "api/openapi.bundled.json"

// diffHeader starts every file section of a unified diff git writes.
const diffHeader = "diff --git "

// FilterDiff returns diff without the file sections the reviewer does not read
// (issue #324, ADR 0033), and the paths it left out, each once and sorted.
//
// The list is fixed here, and no pull request can widen it: the exact path
// api/openapi.bundled.json, and any path ending ".golden" with a directory
// named exactly testdata among its parent directories. Paths are compared as
// git writes them, case-sensitively and uncleaned, so a path with an empty, "."
// or ".." element is never left out. A section goes only when every path its
// "diff --git a/<old> b/<new>" header names is left out, so a rename or copy
// between a reviewed path and a left-out one is reviewed. A header the filter
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
		if paths, ok := sectionPaths(section.String()); ok && !slices.ContainsFunc(paths, isReviewed) {
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

// isReviewed reports whether the reviewer reads the file at path: every path
// but the generated bundle and the golden files of a testdata directory.
func isReviewed(path string) bool {
	if path == generatedBundle {
		return false
	}

	elements := strings.Split(path, "/")
	if slices.ContainsFunc(elements, func(e string) bool { return e == "" || e == "." || e == ".." }) {
		return true
	}

	parents := elements[:len(elements)-1]

	return !strings.HasSuffix(path, ".golden") || !slices.Contains(parents, "testdata")
}
