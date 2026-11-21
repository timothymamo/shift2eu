// Pins issue #381: a runbook step that has the maintainer download a private key leaves the .pem in the download
// directory when the step's block is pasted whole, so every such step must be followed by a fenced shell block of
// its own, "# Mac (admin), then:", that deletes the file and checks it is gone. A deletion only described in a
// comment of another block is not enough: the block runs past the comment.
package docs_test

import (
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// keyDeletionBlockHeader is the first line of the block that deletes a downloaded private key (issue #381). The
// block stands on its own, so a maintainer who pastes the download step's block whole cannot run past the deletion,
// and the header says where in the runbook it runs.
const keyDeletionBlockHeader = "# Mac (admin), then:"

// The rules a step that downloads a private key, or a comment that only describes the deletion, can break. Each
// rule is the whole message of its finding.
const (
	noKeyDeletionBlock = "the key download is not followed by a fenced shell block of its own, starting with " +
		`"` + keyDeletionBlockHeader + `"` + ", that deletes the downloaded key (issue #381)"

	noKeyRemoveLine = "the block starting with " + `"` + keyDeletionBlockHeader + `"` +
		" has no rm line naming a .pem path, so it deletes nothing (issue #381)"

	noKeyGoneCheck = "the block starting with " + `"` + keyDeletionBlockHeader + `"` +
		` does not check the key is gone: it needs an ls line of the .pem path with "# PASS: No such file or directory"` +
		" (issue #381)"

	keyDeletionInAComment = "only a comment inside another block tells the maintainer to delete the downloaded key: " +
		"pasted whole, the block runs past the comment and the .pem stays in the download directory (issue #381)"
)

// The prose that has the maintainer download a private key from an App's page: the step that generates one, the
// parenthetical that says a file downloads, and the rollback's "A new key, generated on the App's page". They are
// matched against a line with its Markdown emphasis and code markers dropped (proseText), so "a **new** private
// key" reads as "a new private key".
var (
	generateAPrivateKey        = regexp.MustCompile(`(?i)generate an? (?:new )?private key`)
	aPEMFileDownloads          = regexp.MustCompile(`(?i)\.pem file downloads`)
	aKeyGeneratedOnTheAppsPage = regexp.MustCompile(`(?i)generated on the App's page`)
)

// readsADownloadedKey matches a shell line that feeds a downloaded private key to a command: an input redirect from
// a .pem under ~/Downloads, where a browser puts it, or from the placeholder path a page uses for the download.
var readsADownloadedKey = regexp.MustCompile(`<\s*\S*(?:Downloads/[^\s]*\.pem|the-downloaded-key\.pem)`)

// deletesTheDownloadedKey matches a comment that only tells the maintainer to delete the downloaded key.
var deletesTheDownloadedKey = regexp.MustCompile(`(?i)delete the downloaded`)

// keyDeletionFinding is one place where a page leaves a downloaded private key behind: the 1-based line of the
// download or of the comment, the rule it breaks, and the line's text.
type keyDeletionFinding struct {
	line int
	rule string
	text string
}

// keyFinding is a finding as the tests compare it: the line and the rule, without the page's prose.
type keyFinding struct {
	line int
	rule string
}

// keyFindings drops the text of findings, which the expected values of the table do not repeat.
func keyFindings(findings []keyDeletionFinding) []keyFinding {
	if len(findings) == 0 {
		return nil
	}

	out := make([]keyFinding, 0, len(findings))
	for _, f := range findings {
		out = append(out, keyFinding{line: f.line, rule: f.rule})
	}
	return out
}

// keyBlock is one fenced code block of a page.
type keyBlock struct {
	open, close int      // the 1-based lines of the opening and the closing fence
	shell       bool     // whether the block holds commands, as shellFence reads its info string
	body        []string // the lines between the fences
	firstBody   int      // the 1-based line of body[0], or 0 for an empty block
}

// keyPage is a page prepared for the scan: its lines, its fenced code blocks, and the block each line sits in.
type keyPage struct {
	lines   []string
	blocks  []keyBlock
	blockAt []int // the index in blocks of the block holding the line, or -1
}

// newKeyPage splits a page into lines and scans its fenced code blocks. A fence the page leaves open runs to the
// end of the page, as GFM reads it.
func newKeyPage(data string) keyPage {
	lines := strings.Split(data, "\n")
	blocks := keyBlocks(lines)

	blockAt := make([]int, len(lines))

	for i := range blockAt {
		blockAt[i] = -1
	}

	for bi, b := range blocks {
		for i := b.open - 1; i < b.close && i < len(lines); i++ {
			blockAt[i] = bi
		}
	}

	return keyPage{lines: lines, blocks: blocks, blockAt: blockAt}
}

// keyBlocks returns the fenced code blocks of lines, in order.
func keyBlocks(lines []string) []keyBlock {
	var (
		blocks []keyBlock
		cur    *keyBlock
		marker byte
		width  int
	)

	for i, line := range lines {
		if cur == nil {
			if m, w, info, ok := openingFence(line); ok {
				cur = &keyBlock{open: i + 1, shell: shellFence(info)}
				marker, width = m, w
			}

			continue
		}

		if closesFence(line, marker, width) {
			cur.close = i + 1
			blocks = append(blocks, *cur)
			cur = nil

			continue
		}

		if len(cur.body) == 0 {
			cur.firstBody = i + 1
		}

		cur.body = append(cur.body, line)
	}

	if cur != nil {
		cur.close = len(lines) + 1
		blocks = append(blocks, *cur)
	}

	return blocks
}

// proseText returns line without the Markdown emphasis and code markers, so a rule written for plain prose matches
// a line that sets a word in bold or quotes a path in code.
func proseText(line string) string {
	return strings.NewReplacer("`", "", "*", "").Replace(line)
}

// proseDownloadsAKey reports whether line, which is no part of a fenced block, has the maintainer download a
// private key.
func proseDownloadsAKey(line string) bool {
	text := proseText(line)

	return generateAPrivateKey.MatchString(text) ||
		aPEMFileDownloads.MatchString(text) ||
		aKeyGeneratedOnTheAppsPage.MatchString(text)
}

// keyDownloads returns the 1-based lines of the page that have the maintainer download a private key: the prose
// that generates one, and the shell line that feeds a downloaded .pem to a command.
func (p keyPage) keyDownloads() []int {
	var downloads []int

	for i, line := range p.lines {
		bi := p.blockAt[i]

		switch {
		case bi < 0:
			if proseDownloadsAKey(line) {
				downloads = append(downloads, i+1)
			}
		case p.blocks[bi].shell && readsADownloadedKey.MatchString(line):
			downloads = append(downloads, i+1)
		}
	}

	return downloads
}

// stepEnd returns the 0-based index of the first line after download (1-based) that ends the step it belongs to,
// or len(p.lines). A step ends at the next heading outside a fenced block: the heading of another section, or of
// another step.
//
// The step does not end at the next numbered step, even though a reader would say it does. The AI reviewer's step 1
// downloads its key ("a .pem file downloads") and step 3 is where it is stored and deleted, so a page that keeps
// the key until step 3 - the only correct order - would otherwise fail on step 1 forever (issue #381).
func (p keyPage) stepEnd(download int) int {
	for i := download; i < len(p.lines); i++ {
		if p.blockAt[i] >= 0 {
			continue
		}

		if _, _, ok := markdownHeading(p.lines[i]); ok {
			return i
		}
	}

	return len(p.lines)
}

// keyDeletionFindings returns every place where the page leaves a downloaded private key behind, in line order:
// one finding per key download without a block of its own that deletes the key and checks it is gone, and one per
// comment that only describes the deletion.
func (p keyPage) keyDeletionFindings() []keyDeletionFinding {
	var findings []keyDeletionFinding

	for _, download := range p.keyDownloads() {
		findings = append(findings, p.deletionFindingsFor(download)...)
	}

	for _, b := range p.blocks {
		if !b.shell || len(b.body) == 0 || strings.HasPrefix(strings.TrimSpace(b.body[0]), keyDeletionBlockHeader) {
			continue
		}

		for off, line := range b.body {
			if strings.HasPrefix(strings.TrimSpace(line), "#") && deletesTheDownloadedKey.MatchString(line) {
				findings = append(findings, keyDeletionFinding{
					line: b.firstBody + off,
					rule: keyDeletionInAComment,
					text: strings.TrimSpace(line),
				})
			}
		}
	}

	slices.SortStableFunc(findings, func(a, b keyDeletionFinding) int { return a.line - b.line })

	return findings
}

// deletionFindingsFor returns what the key download on line download (1-based) lacks: the fenced shell block of
// its own that starts with keyDeletionBlockHeader, or, once there is one, the rm line that deletes the key and the
// ls line that checks it is gone.
func (p keyPage) deletionFindingsFor(download int) []keyDeletionFinding {
	text := strings.TrimSpace(p.lines[download-1])
	missing := keyDeletionFinding{line: download, rule: noKeyDeletionBlock, text: text}
	end := p.stepEnd(download)

	for _, b := range p.blocks {
		if !b.shell || b.open <= download || b.open > end || len(b.body) == 0 {
			continue
		}

		if !strings.HasPrefix(strings.TrimSpace(b.body[0]), keyDeletionBlockHeader) {
			continue
		}

		switch {
		case !slices.ContainsFunc(b.body, removesAKey):
			return []keyDeletionFinding{{line: download, rule: noKeyRemoveLine, text: text}}
		case !slices.ContainsFunc(b.body, checksAKeyIsGone):
			return []keyDeletionFinding{{line: download, rule: noKeyGoneCheck, text: text}}
		default:
			return nil
		}
	}

	return []keyDeletionFinding{missing}
}

// removesAKey reports whether line removes a .pem file: an rm command, with or without sudo, that names one.
func removesAKey(line string) bool {
	return rmPEM.MatchString(strings.TrimSpace(line))
}

// checksAKeyIsGone reports whether line checks that a .pem file is gone: an ls of it whose PASS comment says the
// file does not exist.
func checksAKeyIsGone(line string) bool {
	return lsPEM.MatchString(strings.TrimSpace(line)) && strings.Contains(line, "# PASS: No such file or directory")
}

// rmPEM and lsPEM match an rm and an ls command that name a .pem path.
var (
	rmPEM = regexp.MustCompile(`^(?:sudo\s+)?rm\s.*\.pem`)
	lsPEM = regexp.MustCompile(`^(?:sudo\s+)?ls\s.*\.pem`)
)

// TestEveryKeyDownloadIsFollowedByItsDeletionBlock pins the rule the scan applies to small pages (issue #381): a
// step that downloads a private key needs a block of its own, "# Mac (admin), then:", that removes the .pem and
// checks it is gone, and a comment that only says to delete it is not enough.
func TestEveryKeyDownloadIsFollowedByItsDeletionBlock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		page []string
		want []keyFinding
	}{
		{
			name: "a download step followed by its own deletion block passes",
			page: []string{
				"# Development environment",
				"",
				"**3. Store its secrets:** generate a **new** private key on the App's page.",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.private-key.pem   # PASS: No such file or directory",
				"```",
			},
		},
		{
			name: "a deletion only described in a comment inside the block fails",
			page: []string{
				"# Development environment",
				"",
				"**3. Store its secrets:** generate a **new** private key on the App's page.",
				"",
				"```bash",
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY --env ai-review",
				"# then delete the downloaded .pem, or move it to your password manager",
				"```",
			},
			want: []keyFinding{
				{line: 3, rule: noKeyDeletionBlock},
				{line: 7, rule: keyDeletionInAComment},
			},
		},
		{
			name: "a shell line that feeds the downloaded key to a command is a download of its own",
			page: []string{
				"# Development environment",
				"",
				"**3. Store the key:**",
				"",
				"```bash",
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY < path/to/the-downloaded-key.pem",
				"```",
			},
			want: []keyFinding{{line: 6, rule: noKeyDeletionBlock}},
		},
		{
			name: "the step that stores the key may hold the block that deletes it",
			page: []string{
				"# Development environment",
				"",
				"**1. Create the App.** Generate a private key (a `.pem` file downloads).",
				"",
				"**2. Install it.**",
				"",
				"**3. Store its secrets:**",
				"",
				"```bash",
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY --env ai-review < path/to/the-downloaded-key.pem",
				"```",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.private-key.pem   # PASS: No such file or directory",
				"```",
			},
		},
		{
			name: "a download step with no deletion block fails",
			page: []string{
				"# Development environment",
				"",
				"Generate a private key and keep the `.pem` outside the workspace.",
				"",
				"## Agent permissions",
				"",
				"Nothing else.",
			},
			want: []keyFinding{{line: 3, rule: noKeyDeletionBlock}},
		},
		{
			name: "a page with no key download passes",
			page: []string{
				"# Development environment",
				"",
				"Set the token, then check that the download directory holds nothing.",
				"",
				"```bash",
				"ls ~/Downloads   # PASS: nothing",
				"```",
			},
		},
		{
			name: "a block that deletes the key but does not check it is gone fails",
			page: []string{
				"**2. Delete it:** generate a private key on the App's page.",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.private-key.pem",
				"```",
			},
			want: []keyFinding{{line: 1, rule: noKeyGoneCheck}},
		},
		{
			name: "a block that checks the key is gone but deletes nothing fails",
			page: []string{
				"**2. Delete it:** generate a private key on the App's page.",
				"",
				"```bash",
				"# Mac (admin), then:",
				"ls ~/Downloads/<app>.private-key.pem   # PASS: No such file or directory",
				"```",
			},
			want: []keyFinding{{line: 1, rule: noKeyRemoveLine}},
		},
		{
			name: "a block that starts with another comment is not the deletion block",
			page: []string{
				"**2. Delete it:** generate a private key on the App's page.",
				"",
				"```bash",
				"# Mac, host clone",
				"rm ~/Downloads/<app>.private-key.pem",
				"ls ~/Downloads/<app>.private-key.pem   # PASS: No such file or directory",
				"```",
			},
			want: []keyFinding{{line: 1, rule: noKeyDeletionBlock}},
		},
		{
			name: "a .pem named in a markdown block is prose, not a download",
			page: []string{
				"# Development environment",
				"",
				"```markdown",
				"Generate a private key and keep the `.pem` outside the workspace.",
				"```",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := keyFindings(newKeyPage(strings.Join(tc.page, "\n")).keyDeletionFindings())

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(keyFinding{})); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", tc.name, diff)
			}
		})
	}
}

// TestDevEnvironmentDeletesEveryDownloadedKey pins issue #381 on the page the VM is set up from: every step that
// has the maintainer download a private key - the agents App's, the account's and the AI reviewer's - is followed
// by a block of its own that deletes the .pem and checks it is gone, and no comment only says to delete it.
func TestDevEnvironmentDeletesEveryDownloadedKey(t *testing.T) {
	t.Parallel()

	data, err := fs.ReadFile(os.DirFS(repoRoot), devEnvironmentPage)
	if err != nil {
		t.Fatalf("reading %s: %v", devEnvironmentPage, err)
	}

	page := newKeyPage(string(data))

	if downloads := page.keyDownloads(); len(downloads) == 0 {
		t.Fatalf("%s: found no step that downloads a private key, so this test would accept a page that leaves every key behind: the rules for a download no longer match the page (issue #381)", devEnvironmentPage)
	}

	for _, f := range page.keyDeletionFindings() {
		t.Errorf("%s:%d %s: %s", devEnvironmentPage, f.line, f.rule, f.text)
	}
}
