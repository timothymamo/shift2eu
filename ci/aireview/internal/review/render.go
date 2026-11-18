package review

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
	_, _, _, _ = headSHA, answer, event, diff // the skeleton's stub (step 1)

	return Review{}
}
