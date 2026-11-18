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
//
// Every mention in the text the model wrote (summary, titles, bodies) is
// defused, so the review notifies nobody. The body and each inline comment's
// body are cut to MaxTextRunes with a note that says so; the body keeps its
// first line, which names headSHA, and its closing statement that the review
// is advice and never an approval (ADR 0033).
func Render(headSHA string, answer Answer, event Event, diff string) Review {
	shown := newSideLines(diff)

	var (
		comments []InlineComment
		inBody   []Finding
	)

	for _, f := range answer.Findings {
		if f.Path != "" && f.Line > 0 && shown[diffLine{path: f.Path, line: f.Line}] {
			comments = append(comments, InlineComment{
				Path: f.Path,
				Line: f.Line,
				Body: truncateRunes(findingText(f), MaxTextRunes, truncatedComment),
			})

			continue
		}

		inBody = append(inBody, f)
	}

	head := fmt.Sprintf("**AI review** of %s\n\n", headSHA)
	middle := renderMiddle(answer, event, comments, inBody)

	room := MaxTextRunes - runeCount(head) - runeCount(adviceFooter)

	return Review{
		CommitID: headSHA,
		Event:    event,
		Body:     head + truncateRunes(middle, room, truncatedBody) + adviceFooter,
		Comments: comments,
	}
}

// MaxTextRunes is GitHub's limit on a review's body and on each inline
// comment's body, in characters: a longer text is refused with a 422.
const MaxTextRunes = 65_000

// adviceFooter closes every review's body, whatever Render cuts.
const adviceFooter = "\n---\n\n_This review is advice for the maintainer from the AI reviewer (ADR 0033). " +
	"It never approves: only the maintainer's approval counts._\n"

// The notes that end a text Render cut to GitHub's limit.
const (
	truncatedBody    = "\n\n_The review was truncated here: it is longer than GitHub accepts._\n"
	truncatedComment = "\n\n_This comment was truncated here: it is longer than GitHub accepts._\n"
)

// truncateRunes returns s when it has at most limit runes, and otherwise its
// first runes followed by note, limit runes in all.
func truncateRunes(s string, limit int, note string) string {
	if runeCount(s) <= limit {
		return s
	}

	keep := max(limit-runeCount(note), 0)

	return string([]rune(s)[:keep]) + note
}

// runeCount is the number of characters of s, as GitHub counts them.
func runeCount(s string) int {
	return len([]rune(s))
}

// defuseMentions makes every @ followed by an ASCII letter or digit in s
// notify nobody: it puts a zero-width space after the @, so the handle stays
// readable but is no mention. It is applied to text the model wrote only.
func defuseMentions(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])

		if s[i] == '@' && i+1 < len(s) && isASCIIAlnum(s[i+1]) {
			b.WriteRune(zeroWidthSpace)
		}
	}

	return b.String()
}

// zeroWidthSpace (U+200B) is what defuseMentions puts after an @.
const zeroWidthSpace rune = 0x200B

func isASCIIAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

// renderMiddle renders the body between its first line and adviceFooter: the
// summary, why the review has its event, and the findings not posted inline.
func renderMiddle(answer Answer, event Event, comments []InlineComment, inBody []Finding) string {
	var body strings.Builder

	body.WriteString(defuseMentions(strings.TrimSpace(answer.Summary)))
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

	return body.String()
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

	return fmt.Sprintf("%s: **%s**\n\n%s", head,
		defuseMentions(strings.TrimSpace(f.Title)), defuseMentions(strings.TrimSpace(f.Body)))
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
// prefix, and "" for /dev/null (a deleted file). A path git quotes
// (+++ "b/a b.go", for a space or a non-ASCII byte) is not unquoted: it gives
// "", so a finding on such a file falls back to the body, which is safe.
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
