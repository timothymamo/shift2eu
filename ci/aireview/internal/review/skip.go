package review

import (
	"errors"
	"fmt"
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
// review body's own text, which may name a path in a code span.
type skipped struct {
	skip Skip
	body string
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
// while usable other parts got a usable answer, whose findings the review
// leaves out.
func skipNoUsableAnswer(check string, index, count, usable int) skipped {
	where := ""
	if count > 1 {
		where = fmt.Sprintf(" for part %d of %d of the diff", index, count)
	}

	message := fmt.Sprintf("The model gave no usable answer twice%s (shape check: %s), "+
		"so the AI review was skipped.", where, check)

	// Program text, never the model's: the one cause of this skip a pull
	// request can bring about itself (issue #376, ADR 0033).
	body := message
	if left := leftOutParts(usable); left != "" {
		body += "\n\n" + left
	}

	body += "\n\n" + injectionWarning

	return skipped{skip: Skip{Reason: SkipNoUsableAnswer, Message: message}, body: body}
}

// leftOutParts returns the fixed sentence that says how many other parts of a
// split diff got a usable answer whose findings the skipped review leaves out,
// and asks the maintainer to review those parts by hand, and "" when none did
// (issue #367). It holds this package's words and the count alone, never text
// of the model's answer.
func leftOutParts(usable int) string {
	switch {
	case usable <= 0:
		return ""
	case usable == 1:
		return "1 other part of the diff was reviewed with a usable answer; its findings are not shown, " +
			"so review that part by hand."
	default:
		return fmt.Sprintf("%d other parts of the diff were reviewed with a usable answer; their findings are not shown, "+
			"so review those parts by hand.", usable)
	}
}

// skippedReview returns the review a skipped run posts on headSHA: a comment
// that says the review was skipped and why, holds no finding and no inline
// comment, and is never a change request.
func skippedReview(headSHA string, s skipped) Review {
	var body strings.Builder

	fmt.Fprintf(&body, "**AI review** of %s\n\n", headSHA)
	body.WriteString("### AI review skipped\n\n")
	body.WriteString(s.body)
	body.WriteString("\n\nThis review holds no finding, and it is no review of the change: the maintainer reviews " +
		"it without the AI reviewer's advice. The check passes so that this does not read as a failure of " +
		"the code, and its warning says the review was skipped. Re-run it to ask for a review again.\n")
	body.WriteString(adviceFooter)

	return Review{CommitID: headSHA, Event: EventComment, Body: body.String()}
}
