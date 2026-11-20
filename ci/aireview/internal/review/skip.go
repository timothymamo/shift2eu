package review

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// SkipReason is why a run ended in the skipped outcome (issues #367, #376,
// ADR 0033).
type SkipReason string

// The two causes of the skipped outcome. No other failure takes it.
const (
	// SkipTooLarge is a diff too large to review: a raw diff over
	// MaxRawDiffBytes, a single file's section over MaxDiffBytes, or a diff
	// that needs more than MaxDiffParts parts.
	SkipTooLarge SkipReason = "too-large"
	// SkipNoUsableAnswer is a model that answered twice in turn, for the run
	// or one of its parts, with an answer of the wrong shape.
	SkipNoUsableAnswer SkipReason = "no-usable-answer"
)

// Skip is a run that reviewed nothing and says why: its cause, and a message of
// one line built from this package's own words and numbers alone, never text
// of the pull request or of the model's answer. ci/aireview's main prints it
// as a warning (SkipAnnotation) and a job summary line (SkipSummaryLine).
type Skip struct {
	Reason  SkipReason
	Message string
}

// SkipAnnotation returns the workflow warning a skipped run prints: the title
// "AI review skipped" and skip's message, escaped as every annotation's data is
// (AnnotationData), so it is one line that cannot start a command of its own.
func SkipAnnotation(skip Skip) string {
	return "::warning title=AI review skipped::" + AnnotationData(skip.Message)
}

// SkipSummaryLine returns the one line a skipped run appends to the job
// summary, without its line break: that the AI review was skipped, and why.
func SkipSummaryLine(skip Skip) string {
	return "**AI review skipped:** " + oneLine(skip.Message)
}

// injectionWarning is the fixed sentence the skipped review of two unusable
// answers carries, and the skip of a diff too large does not: content in the
// diff can cause this skip (issue #376, ADR 0033).
const injectionWarning = "Content in the diff, including instructions aimed at the reviewer, can make the model " +
	"answer unusably: read the diff with prompt injection in mind."

// skipped is a run's skipped outcome on its way to the post: the Skip, and the
// review body's own text, which may name a path in a code span, and the fixed
// sentences that follow it, which skippedReview never cuts.
type skipped struct {
	skip Skip
	body string
	tail string
}

// skipTooLarge returns the skipped outcome of err, an error wrapping
// ErrDiffTooLarge from the client's read of the raw diff or from SplitDiff, and
// false for any other error.
func skipTooLarge(err error) (skipped, bool) {
	if !errors.Is(err, ErrDiffTooLarge) {
		return skipped{}, false
	}

	var split *diffTooLargeError
	if !errors.As(err, &split) {
		message := fmt.Sprintf("The pull request's diff is over %s bytes, the most the AI reviewer reads, "+
			"so the AI review was skipped.", thousands(MaxRawDiffBytes))

		return skipped{skip: Skip{Reason: SkipTooLarge, Message: message}, body: message}, true
	}

	if split.parts {
		message := fmt.Sprintf("The diff the AI review reads, once generated and golden files are left out, is %s bytes, "+
			"and cut at file boundaries it does not fit in %d parts of at most %s bytes each, "+
			"so the AI review was skipped.", thousands(split.size), MaxDiffParts, thousands(MaxDiffBytes))

		return skipped{skip: Skip{Reason: SkipTooLarge, Message: message}, body: message}, true
	}

	message := fmt.Sprintf("The diff of a single file is %s bytes, over the %s bytes one model call reads, "+
		"and a file's diff is never split, so the AI review was skipped.", thousands(split.size), thousands(MaxDiffBytes))

	// The path is the pull request's: shown in the review alone, escaped and
	// cut as the "Not reviewed" note shows one, and never in the message,
	// which reaches the job's log.
	body := message
	if split.file != "" {
		body = fmt.Sprintf("The diff of a single file, %s, is %s bytes, over the %s bytes one model call reads, "+
			"and a file's diff is never split, so the AI review was skipped.",
			shortPathSpan(split.file), thousands(split.size), thousands(MaxDiffBytes))
	}

	return skipped{skip: Skip{Reason: SkipTooLarge, Message: message}, body: body}, true
}

// skipNoUsableAnswer returns the skipped outcome of a model that gave no usable
// answer twice, for part index of count (count 1 for a diff of one part), whose
// second answer failed the shape check check, one of this package's constants,
// while the answered parts got a usable answer, whose findings the review
// leaves out and summarises (answeredSummary, issue #388). The summary goes in
// the review's body alone, never in the message, which reaches the job's log.
func skipNoUsableAnswer(check string, index, count int, answered []answeredPart) skipped {
	where := ""
	if count > 1 {
		where = fmt.Sprintf(" for part %d of %d of the diff", index, count)
	}

	message := fmt.Sprintf("The model gave no usable answer twice%s (shape check: %s), "+
		"so the AI review was skipped.", where, check)

	// Program text, never the model's: the one cause of this skip a pull
	// request can bring about itself (issue #376, ADR 0033).
	body := message
	if left := leftOutParts(len(answered)); left != "" {
		body += "\n\n" + left
	}

	if summary := answeredSummary(answered, count); summary != "" {
		body += "\n\n" + summary
	}

	return skipped{
		skip: Skip{Reason: SkipNoUsableAnswer, Message: message},
		body: body,
		tail: "\n\n" + injectionWarning,
	}
}

// answeredPart is a part of a split diff that got a usable answer while
// another part did not: its number (from 1), its findings, and the paths its
// own diff's file headers name, which decide whether a finding's path is
// shown (issue #388).
type answeredPart struct {
	index    int
	findings []Finding
	files    map[string]bool
}

// partFiles returns the paths, old and new, that the file headers of part, a
// part of a split diff, name and sectionPaths can read with certainty. A path
// it cannot read is in no part, so a finding naming it reads as outside its
// part, which is safe.
func partFiles(part string) map[string]bool {
	files := map[string]bool{}

	for _, section := range diffSections(part) {
		if paths, ok := sectionPaths(section); ok {
			for _, p := range paths {
				files[p] = true
			}
		}
	}

	return files
}

// MaxListedBlocking is how many blocking findings the summary of a skipped
// split review lists across the parts that answered; one constant line counts
// the rest (audit M1, issue #388).
const MaxListedBlocking = 50

// answeredSummary returns the skipped review's summary of the parts of count
// that answered usably, and "" when none did: the fixed heading "Findings in
// the parts that answered", then per part, in the parts' order, how many of
// its findings block and how many do not, and beneath it one line per
// blocking finding with its category and its location (issue #388, ADR 0033).
// The category is this package's constant, CategoryUncategorised reading
// "other"; the location is the finding's path, and its line when it has one,
// only when the path is a file of that part's own diff, "no file" when the
// finding has no path, and "a file outside the part" otherwise. No title,
// body, summary or other text of the model's answer is shown: the model
// chooses at most the counts and which of those constants and of the part's
// paths and lines appear. The number of findings is the model's choice too,
// so at most MaxListedBlocking blocking findings are listed across all parts,
// and one constant line after the parts counts the rest (audit M1); the
// counts stay exact.
func answeredSummary(answered []answeredPart, count int) string {
	if len(answered) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("**Findings in the parts that answered:**\n")

	listed, unlisted := 0, 0

	for _, part := range answered {
		blocking := countBlocking(part.findings)

		fmt.Fprintf(&b, "\n- Part %d of %d: %d blocking, %d non-blocking.",
			part.index, count, blocking, len(part.findings)-blocking)

		for _, f := range part.findings {
			if !f.Blocking() {
				continue
			}

			if listed == MaxListedBlocking {
				unlisted++

				continue
			}

			listed++

			fmt.Fprintf(&b, "\n  - blocking: category %s%s", summaryCategory(f.Category), summaryLocation(f, part.files))
		}
	}

	if unlisted > 0 {
		fmt.Fprintf(&b, "\n- And %d more blocking %s across the parts, not listed.",
			unlisted, plural(unlisted, "finding", "findings"))
	}

	return b.String()
}

// summaryCategory returns c as the summary names it: CategoryUncategorised as
// "other", any other category, always one of this package's constants, as it
// is.
func summaryCategory(c Category) string {
	if c == CategoryUncategorised || !slices.Contains(categories, c) {
		return "other"
	}

	return string(c)
}

// summaryLocation returns where the summary says finding f of a part is, as
// the text that follows its category: ", no file" when f has no path,
// " at a file outside the part" when f's path is not one of files, the paths
// of the part's own diff, and otherwise " at " and the path in a code span,
// escaped and cut as the "Not reviewed" note shows one (shortPathSpan),
// followed outside the span by ":" and the line when f has one.
func summaryLocation(f Finding, files map[string]bool) string {
	switch {
	case f.Path == "":
		return ", no file"
	case !files[f.Path]:
		return " at a file outside the part"
	case f.Line > 0:
		return " at " + shortPathSpan(f.Path) + ":" + strconv.Itoa(f.Line)
	default:
		return " at " + shortPathSpan(f.Path)
	}
}

// leftOutParts returns the fixed sentence that says how many other parts of a
// split diff got a usable answer, says that only the summary below
// (answeredSummary) shows their findings, never the findings' text, and asks
// the maintainer to review those parts by hand, and "" when none did (issues
// #367, #388). It holds this package's words and the count alone, never text
// of the model's answer.
func leftOutParts(usable int) string {
	switch {
	case usable <= 0:
		return ""
	case usable == 1:
		return "1 other part of the diff was reviewed with a usable answer; its findings' text is not shown, " +
			"only the summary below, so review that part by hand."
	default:
		return fmt.Sprintf("%d other parts of the diff were reviewed with a usable answer; their findings' text "+
			"is not shown, only the summary below, so review those parts by hand.", usable)
	}
}

// skippedReview returns the review a skipped run posts on headSHA: a comment
// that says the review was skipped and why, holds no finding and no inline
// comment, and is never a change request. Its body is at most MaxTextUnits:
// s.body is cut, as Render cuts a review's, and the head, s.tail, the closing
// sentences and the footer never are (audit M1).
func skippedReview(headSHA string, s skipped) Review {
	head := fmt.Sprintf("**AI review** of %s\n\n### AI review skipped\n\n", headSHA)
	// The truncate is a backstop: answeredSummary's cap keeps s.body far below
	// room today, so this branch is not reached; it holds the limit should the
	// body ever grow.
	room := MaxTextUnits - utf16Len(head) - utf16Len(s.tail) - utf16Len(skippedClosing) - utf16Len(adviceFooter)
	body := head + truncate(s.body, room, truncatedBody) + s.tail + skippedClosing + adviceFooter

	return Review{CommitID: headSHA, Event: EventComment, Body: body}
}

// skippedClosing is the fixed text that ends every skipped review before the
// footer.
const skippedClosing = "\n\nThis review holds no finding, and it is no review of the change: the maintainer reviews " +
	"it without the AI reviewer's advice. The check passes so that this does not read as a failure of " +
	"the code, and its warning says the review was skipped. Re-run it to ask for a review again.\n"
