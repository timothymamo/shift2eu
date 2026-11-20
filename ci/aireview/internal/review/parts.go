package review

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxDiffParts is the most parts SplitDiff splits a diff into (issue #367, ADR
// 0033): MaxRawDiffBytes is four times MaxDiffBytes, so four full parts hold
// any diff the client reads, and four parts bound a run at four model calls
// (plus a retry each).
const MaxDiffParts = 4

// SplitDiff splits diff, a diff FilterDiff kept, into the parts the review asks
// the model about, one call each (issue #367). A diff of at most MaxDiffBytes,
// the empty diff included, is one part equal to diff. A larger one is cut at
// its "diff --git " headers, so no file's section is ever split: the sections
// are taken in the diff's order, text before the first one going with it, and
// a new part starts when the next section would take the current part over
// MaxDiffBytes. The parts joined are diff byte for byte. It returns an error
// wrapping ErrDiffTooLarge, and no parts, when one section is over MaxDiffBytes
// on its own (a hunk cut short is no diff) or the diff needs more than
// MaxDiffParts parts. SplitDiff depends on diff alone.
func SplitDiff(diff string) ([]string, error) {
	if len(diff) <= MaxDiffBytes {
		return []string{diff}, nil
	}

	var (
		parts   []string
		current strings.Builder
	)

	for _, section := range diffSections(diff) {
		if len(section) > MaxDiffBytes {
			return nil, &diffTooLargeError{size: len(section), file: sectionFile(section)}
		}

		if current.Len() > 0 && current.Len()+len(section) > MaxDiffBytes {
			parts = append(parts, current.String())
			current.Reset()
		}

		current.WriteString(section)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	if len(parts) > MaxDiffParts {
		return nil, &diffTooLargeError{size: len(diff), parts: true}
	}

	return parts, nil
}

// diffSections returns diff cut before each line that starts with "diff --git
// ", as FilterDiff cuts it: text before the first such line is a section of its
// own. The sections joined are diff.
func diffSections(diff string) []string {
	var (
		sections []string
		start    int
		offset   int
	)

	for line := range strings.SplitAfterSeq(diff, "\n") {
		if strings.HasPrefix(line, diffHeader) && offset > start {
			sections = append(sections, diff[start:offset])
			start = offset
		}

		offset += len(line)
	}

	if offset > start {
		sections = append(sections, diff[start:offset])
	}

	return sections
}

// sectionFile returns the new path a section's header names, and "" when the
// header cannot be read with certainty (sectionPaths).
func sectionFile(section string) string {
	paths, ok := sectionPaths(section)
	if !ok {
		return ""
	}

	return paths[1]
}

// diffTooLargeError is a diff the review cannot read in at most MaxDiffParts
// parts of at most MaxDiffBytes: one file's section over MaxDiffBytes (file is
// its path, untrusted and "" when unknown, size the section's bytes), or a diff
// that needs more than MaxDiffParts parts (parts true, size the diff's bytes).
// It wraps ErrDiffTooLarge. Its text names no path: the path is the pull
// request's, and only the review shows it, escaped (skippedTooLarge).
type diffTooLargeError struct {
	size  int
	file  string
	parts bool
}

func (e *diffTooLargeError) Error() string {
	if e.parts {
		return fmt.Sprintf("%s: %d bytes need more than %d parts of at most %d bytes",
			ErrDiffTooLarge, e.size, MaxDiffParts, MaxDiffBytes)
	}

	return fmt.Sprintf("%s: one file's diff is %d bytes, over %d", ErrDiffTooLarge, e.size, MaxDiffBytes)
}

func (e *diffTooLargeError) Unwrap() error { return ErrDiffTooLarge }

// thousands returns n in decimal with a comma between groups of three digits,
// as the review and its notices write a size: 300000 as "300,000". A size is
// never negative.
func thousands(n int) string {
	digits := strconv.Itoa(max(n, 0))

	var b strings.Builder

	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(d)
	}

	return b.String()
}
