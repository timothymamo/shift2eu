package review

import (
	"fmt"
	"strconv"
	"strings"
)

// Review is one review to post: the commit it reviewed, its event, its body and
// its inline comments.
type Review struct {
	CommitID string
	Event    Event
	Body     string
	Comments []InlineComment
}

// InlineComment is a review comment on one line of the new side of the diff.
type InlineComment struct {
	Path string
	Line int
	Body string
}

// Render renders the review of headSHA. The body names headSHA, holds
// answer's summary, says the review is advice and never an approval, and
// lists every finding with its severity and category, a blocking one marked
// "blocking"; when a finding is blocking and event is EventComment it also says
// that the review would have requested changes. A finding with a path and line
// that diff shows on its new side (an added or a context line of a hunk of that
// file) becomes an inline comment on that line, with its severity, and is not
// repeated in the body; every other finding (no path, no line, a line or file
// outside the diff) is listed in the body with its path and line, if any.
// Inline comments keep the findings' order. CommitID is headSHA and Event is
// event, unchanged: Render refuses nothing, the poster does.
func Render(headSHA string, answer Answer, event Event, diff string) Review {
	shown := newSideLines(diff)

	var (
		comments []InlineComment
		inBody   []Finding
	)

	for _, f := range answer.Findings {
		if f.Path != "" && f.Line > 0 && shown[diffLine{path: f.Path, line: f.Line}] {
			comments = append(comments, InlineComment{Path: f.Path, Line: f.Line, Body: findingText(f)})

			continue
		}

		inBody = append(inBody, f)
	}

	var body strings.Builder

	fmt.Fprintf(&body, "**AI review** of %s\n\n", headSHA)
	body.WriteString(strings.TrimSpace(answer.Summary))
	body.WriteString("\n\n")
	body.WriteString(eventReason(answer.Findings, event))
	body.WriteString("\n")

	if len(inBody) > 0 {
		if len(comments) > 0 {
			fmt.Fprintf(&body, "\n### Other findings\n\n%d findings are inline comments on the diff; these are not on a line of it.\n", len(comments))
		} else {
			body.WriteString("\n### Findings\n")
		}

		for _, f := range inBody {
			body.WriteString("\n---\n\n")

			if f.Path != "" {
				where := f.Path
				if f.Line > 0 {
					where += ":" + strconv.Itoa(f.Line)
				}

				fmt.Fprintf(&body, "`%s`\n\n", strings.ReplaceAll(where, "`", "'"))
			}

			body.WriteString(findingText(f))
			body.WriteString("\n")
		}
	} else if len(comments) > 0 {
		fmt.Fprintf(&body, "\n%d findings are inline comments on the diff.\n", len(comments))
	}

	body.WriteString("\n---\n\n_This review is advice for the maintainer from the AI reviewer (ADR 0033). " +
		"It never approves: only the maintainer's approval counts._\n")

	return Review{CommitID: headSHA, Event: event, Body: body.String(), Comments: comments}
}

// eventReason says why the review has its event.
func eventReason(findings []Finding, event Event) string {
	blocking := countBlocking(findings)

	switch {
	case blocking == 0:
		return fmt.Sprintf("%d findings, none blocking.", len(findings))
	case event == EventRequestChanges:
		return fmt.Sprintf("%d findings, %d blocking: this review requests changes.", len(findings), blocking)
	default:
		return fmt.Sprintf("%d findings, %d blocking. Change requests are off (AI_REVIEW_REQUEST_CHANGES), "+
			"so this review is a comment; with them on, it would have requested changes.", len(findings), blocking)
	}
}

// findingText renders one finding: its severity, category, whether it blocks,
// its title and its body.
func findingText(f Finding) string {
	head := fmt.Sprintf("**%s** (%s)", f.Severity, f.Category)
	if f.Blocking() {
		head += ", **blocking**"
	}

	return fmt.Sprintf("%s: **%s**\n\n%s", head, strings.TrimSpace(f.Title), strings.TrimSpace(f.Body))
}

// diffLine is one line of a file on the new side of a diff.
type diffLine struct {
	path string
	line int
}

// newSideLines returns the lines of the unified diff that GitHub can comment on
// with side RIGHT: the added and context lines of each hunk, by the file's path
// on the new side and its new line number.
func newSideLines(diff string) map[diffLine]bool {
	shown := map[diffLine]bool{}

	var (
		path             string
		newLine          int
		oldLeft, newLeft int
	)

	mark := func() {
		if path != "" {
			shown[diffLine{path: path, line: newLine}] = true
		}
	}

	for _, text := range strings.Split(diff, "\n") {
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(text, "+"):
				mark()
				newLine++
				newLeft--
			case strings.HasPrefix(text, "-"):
				oldLeft--
			case strings.HasPrefix(text, `\`):
				// "\ No newline at end of file": no line of either side.
			default:
				// A context line; an empty one is a context line whose
				// leading space was lost.
				mark()
				newLine++
				newLeft--
				oldLeft--
			}

			continue
		}

		switch {
		case strings.HasPrefix(text, "diff --git "):
			path = ""
		case strings.HasPrefix(text, "+++ "):
			path = newSidePath(strings.TrimPrefix(text, "+++ "))
		case strings.HasPrefix(text, "@@ "):
			newLine, oldLeft, newLeft = parseHunkHeader(text)
		}
	}

	return shown
}

// newSidePath returns the path a "+++ " line names: "b/<path>" without its
// prefix, and "" for /dev/null (a deleted file).
func newSidePath(name string) string {
	name, _, _ = strings.Cut(name, "\t")

	if !strings.HasPrefix(name, "b/") {
		return ""
	}

	return strings.TrimPrefix(name, "b/")
}

// parseHunkHeader reads "@@ -a,b +c,d @@ ...": the first new line c and the
// old and new line counts b and d (1 when left out). A header it cannot read
// gives no lines.
func parseHunkHeader(text string) (start, oldCount, newCount int) {
	fields := strings.Fields(text)
	if len(fields) < 4 || fields[0] != "@@" || fields[3] != "@@" {
		return 0, 0, 0
	}

	_, oldCount, okOld := parseRange(fields[1], "-")
	start, newCount, okNew := parseRange(fields[2], "+")

	if !okOld || !okNew {
		return 0, 0, 0
	}

	return start, oldCount, newCount
}

// parseRange reads "<sign>start[,count]".
func parseRange(field, sign string) (start, count int, ok bool) {
	rest, found := strings.CutPrefix(field, sign)
	if !found {
		return 0, 0, false
	}

	first, second, hasCount := strings.Cut(rest, ",")

	start, err := strconv.Atoi(first)
	if err != nil || start < 0 {
		return 0, 0, false
	}

	count = 1

	if hasCount {
		count, err = strconv.Atoi(second)
		if err != nil || count < 0 {
			return 0, 0, false
		}
	}

	return start, count, true
}
