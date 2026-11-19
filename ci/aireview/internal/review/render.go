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
// that the review would have requested changes, and when a finding is
// CategoryUncategorised it says, after the event's reason, how many are. Counts
// say "1 finding" and "N findings". A finding with a path and line
// that diff shows on its new side (an added or a context line of a hunk of that
// file) becomes an inline comment on that line, with its severity, and is not
// repeated in the body; every other finding (no path, no line, a line or file
// outside the diff) is listed in the body with its path and line, if any.
// Inline comments keep the findings' order. CommitID is headSHA and Event is
// event, unchanged: Render refuses nothing, the poster does.
//
// Every mention in the text the model wrote (summary, titles, bodies, the
// paths listed in the body), an HTML character reference to an @ included, is
// defused, so the review notifies nobody; a path listed in the body is kept on
// its one line. The body and each inline comment's body are cut to
// MaxTextUnits with a note that says so, a code fence the cut leaves open
// closed before it; the body keeps its first line, which names headSHA, and
// its closing statement that the review is advice and never an approval
// (ADR 0033).
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
				Body: truncate(findingText(f), MaxTextUnits, truncatedComment),
			})

			continue
		}

		inBody = append(inBody, f)
	}

	head := fmt.Sprintf("**AI review** of %s\n\n", headSHA)
	middle := renderMiddle(answer, event, comments, inBody)

	room := MaxTextUnits - utf16Len(head) - utf16Len(adviceFooter)

	return Review{
		CommitID: headSHA,
		Event:    event,
		Body:     head + truncate(middle, room, truncatedBody) + adviceFooter,
		Comments: comments,
	}
}

// MaxTextUnits caps a review's body and each inline comment's body, in UTF-16
// code units, which is how GitHub counts: a character outside the BMP (an
// emoji) counts 2. The cap is 65,000, kept under GitHub's limit of 65,536
// characters with a margin; GitHub refuses a longer text with a 422.
const MaxTextUnits = 65_000

// adviceFooter closes every review's body, whatever Render cuts.
const adviceFooter = "\n---\n\n_This review is advice for the maintainer from the AI reviewer (ADR 0033). " +
	"It never approves: only the maintainer's approval counts._\n"

// The notes that end a text Render cut to GitHub's limit.
const (
	truncatedBody    = "\n\n_The review was truncated here: it is longer than GitHub accepts._\n"
	truncatedComment = "\n\n_This comment was truncated here: it is longer than GitHub accepts._\n"
)

// truncate returns s when it is at most limit UTF-16 code units, and otherwise
// its first characters followed by note, at most limit units in all. It cuts on
// a character boundary, and when the cut lands inside a code fence it closes
// the fence before note, so note and what follows the text render outside it.
func truncate(s string, limit int, note string) string {
	if utf16Len(s) <= limit {
		return s
	}

	room := max(limit-utf16Len(note), 0)
	budget := room

	for {
		kept := prefixUnits(s, budget)
		closer := fenceCloser(kept)

		if utf16Len(kept)+utf16Len(closer) <= room || budget == 0 {
			return kept + closer + note
		}

		budget = max(budget-utf16Len(closer), 0)
	}
}

// prefixUnits returns the longest prefix of s, cut on a character boundary,
// that is at most limit UTF-16 code units.
func prefixUnits(s string, limit int) string {
	units := 0

	for i, r := range s {
		units += runeUnits(r)
		if units > limit {
			return s[:i]
		}
	}

	return s
}

// utf16Len is the length of s in UTF-16 code units, as GitHub counts it: a
// character outside the BMP counts 2, every other character (a byte that is no
// valid UTF-8 included, which is sent as U+FFFD) counts 1.
func utf16Len(s string) int {
	units := 0

	for _, r := range s {
		units += runeUnits(r)
	}

	return units
}

// runeUnits is the number of UTF-16 code units r takes.
func runeUnits(r rune) int {
	if r > 0xFFFF {
		return 2
	}

	return 1
}

// fenceCloser returns the line that closes the code fence s leaves open, with
// the line break before it, and "" when s leaves no fence open. A fence opens
// on a line of at most three spaces' indent and then three or more backticks
// or tildes, and closes on such a line of the same character, at least as long,
// with nothing after it but spaces (CommonMark).
func fenceCloser(s string) string {
	var (
		fenceChar byte
		fenceLen  int
	)

	for line := range strings.SplitSeq(s, "\n") {
		c, n, rest := fenceMarker(line)
		if n == 0 {
			continue
		}

		switch {
		case fenceLen == 0:
			if c == '`' && strings.ContainsRune(rest, '`') {
				continue // a backtick fence's info string holds no backtick
			}

			fenceChar, fenceLen = c, n
		case c == fenceChar && n >= fenceLen && strings.TrimSpace(rest) == "":
			fenceLen = 0
		}
	}

	if fenceLen == 0 {
		return ""
	}

	return "\n" + strings.Repeat(string(fenceChar), fenceLen)
}

// fenceMarker reads a code fence marker at the start of line: its character,
// its length and the text after it. A line that holds no marker gives length 0.
func fenceMarker(line string) (c byte, n int, rest string) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, ""
	}

	c = trimmed[0]
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}

	if n < 3 {
		return 0, 0, ""
	}

	return c, n, trimmed[n:]
}

// defuseMentions makes every mention in s notify nobody. It puts a zero-width
// space after every @ followed by an ASCII letter or digit, so the handle stays
// readable but is no mention, and it escapes the & of every HTML character
// reference to an @ (&#64;, &#x40;, &commat; and their variants) as &amp;,
// since GitHub renders such a reference as the @ it names: the reference then
// shows as written and notifies nobody. It is applied to text the model wrote
// only.
func defuseMentions(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '&' && isAtReference(s[i+1:]) {
			b.WriteString("&amp;")

			continue
		}

		b.WriteByte(s[i])

		if s[i] == '@' && i+1 < len(s) && isASCIIAlnum(s[i+1]) {
			b.WriteRune(zeroWidthSpace)
		}
	}

	return b.String()
}

// isAtReference reports whether s, the text after an &, starts an HTML
// character reference to an @: a decimal (#64) or a hexadecimal (#x40, #X40)
// one with any leading zeros, with or without its semicolon, or the named one
// (commat;), in any case. A numeric reference followed by more digits names
// another character and is left alone.
func isAtReference(s string) bool {
	if len(s) >= len("commat;") && strings.EqualFold(s[:len("commat;")], "commat;") {
		return true
	}

	rest, ok := strings.CutPrefix(s, "#")
	if !ok {
		return false
	}

	digit, base, want := isDecimalDigit, 10, int64(64)

	if len(rest) > 0 && (rest[0] == 'x' || rest[0] == 'X') {
		rest, digit, base, want = rest[1:], isHexDigit, 16, 0x40
	}

	end := 0
	for end < len(rest) && digit(rest[end]) {
		end++
	}

	if end == 0 {
		return false
	}

	digits := strings.TrimLeft(rest[:end], "0")
	if digits == "" || len(digits) > 4 {
		return false
	}

	value, err := strconv.ParseInt(digits, base, 32)

	return err == nil && value == want
}

func isDecimalDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func isHexDigit(c byte) bool {
	return isDecimalDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// oneLine returns s with every control character (below 0x20, and DEL) as a
// space, so text the model wrote that Render puts on one line stays there.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F {
			return ' '
		}

		return r
	}, s)
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

	if n := countUncategorised(answer.Findings); n > 0 {
		fmt.Fprintf(&body, "%s had a missing or unrecognised category and %s shown as uncategorised.\n",
			findingsPhrase(n), plural(n, "is", "are"))
	}

	if len(inBody) > 0 {
		if len(comments) > 0 {
			below := "The finding below is"
			if len(inBody) > 1 {
				below = fmt.Sprintf("The %d findings below are", len(inBody))
			}

			fmt.Fprintf(&body, "\n### Other findings\n\n%s. %s not on a line of it.\n", inlineSentence(len(comments)), below)
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

				// ParseAnswer refuses a path with a control character, but
				// Render refuses nothing: the path stays on its line and
				// notifies nobody whatever reaches it.
				fmt.Fprintf(&body, "`%s`\n\n", defuseMentions(oneLine(strings.ReplaceAll(where, "`", "'"))))
			}

			body.WriteString(findingText(f))
			body.WriteString("\n")
		}
	} else if len(comments) > 0 {
		fmt.Fprintf(&body, "\n%s.\n", inlineSentence(len(comments)))
	}

	return body.String()
}

// inlineSentence says that n findings are inline comments, without its full
// stop.
func inlineSentence(n int) string {
	if n == 1 {
		return "1 finding is an inline comment on the diff"
	}

	return fmt.Sprintf("%d findings are inline comments on the diff", n)
}

// findingsPhrase returns "1 finding" or "N findings".
func findingsPhrase(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "finding", "findings"))
}

// plural returns one when n is 1 and many otherwise.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}

// countUncategorised returns how many of fs have CategoryUncategorised.
func countUncategorised(fs []Finding) int {
	n := 0

	for _, f := range fs {
		if f.Category == CategoryUncategorised {
			n++
		}
	}

	return n
}

// eventReason says why the review has its event.
func eventReason(fs []Finding, event Event) string {
	blocking := countBlocking(fs)

	switch {
	case blocking == 0:
		return findingsPhrase(len(fs)) + ", none blocking."
	case event == EventRequestChanges:
		return fmt.Sprintf("%s, %d blocking: this review requests changes.", findingsPhrase(len(fs)), blocking)
	default:
		return fmt.Sprintf("%s, %d blocking. Change requests are off (AI_REVIEW_REQUEST_CHANGES), "+
			"so this review is a comment; with them on, it would have requested changes.", findingsPhrase(len(fs)), blocking)
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
