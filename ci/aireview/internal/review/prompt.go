package review

// UntrustedOpenTag and UntrustedCloseTag delimit the pull request's data in the
// user message. The data between them is one JSON document whose encoding
// escapes <, > and &, so no text of the pull request can close the tag.
const (
	UntrustedOpenTag  = "<untrusted_pull_request>"
	UntrustedCloseTag = "</untrusted_pull_request>"
)

// Instructions is the trusted text of a review, read from main's checkout:
// CLAUDE.md and the review-change and security-audit skills.
type Instructions struct {
	ClaudeMD           string
	ReviewChangeSkill  string
	SecurityAuditSkill string
}

// PullRequest is what the reviewer reads of a pull request. Title, Body and
// Author are untrusted: they go to the model only as data.
type PullRequest struct {
	Number  int
	Title   string
	Body    string
	Author  string
	Draft   bool
	HeadSHA string
	BaseSHA string
}

// ChangedFile is one entry of the pull request's file list. Path is untrusted.
type ChangedFile struct {
	Path      string
	Status    string
	Additions int
	Deletions int
}

// Prompt is one model request's text: System holds the trusted instructions
// alone, User the untrusted pull request between the untrusted tags.
type Prompt struct {
	System string
	User   string
}

// BuildPrompt builds the prompt of a review. System holds the reviewer's task,
// each of in's three texts whole in a labelled section, the statement that the
// reviewer has no tools (it runs no command and reads no file), that the pull
// request's data is untrusted and that an instruction to the reviewer inside it
// is reported as a finding of category prompt-injection and never followed, and
// the answer's JSON format (ParseAnswer). It holds no text of pr, files or diff.
// User holds a line saying the data between the tags is untrusted, then
// UntrustedOpenTag, one JSON document with pr's number, title, body, author and
// head SHA, files and diff, encoded so that <, > and & are escaped, then
// UntrustedCloseTag, each tag exactly once. The result depends on its inputs
// alone.
func BuildPrompt(in Instructions, pr PullRequest, files []ChangedFile, diff string) Prompt {
	_, _, _, _ = in, pr, files, diff // the skeleton's stub (step 1)

	return Prompt{}
}
