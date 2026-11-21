// Pins issue #381: a runbook step that has the maintainer download a private key leaves the .pem in the download
// directory when the step's block is pasted whole, so every such step must be followed by a fenced shell block of
// its own, "# Mac (admin), then:", that deletes the file, checks it is gone, and looks for it in the download
// directory and the Trash. A deletion only described in a comment of another block is not enough: the block runs
// past the comment. One block serves one download, so a step that downloads a key twice needs two blocks, and the
// block's rm and ls name the same path, the one a shell line that reads the key names.
package docs_test

import (
	"io/fs"
	"maps"
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
		", the same .pem path its rm names, and for a shell line that reads the key the path that line reads (issue #381)"

	noKeyTrashCheck = "the block starting with " + `"` + keyDeletionBlockHeader + `"` +
		` does not look for the key in the download directory and the Trash: it needs an ` +
		`"ls ~/Downloads ~/.Trash | grep private-key" line with "# PASS: nothing" (issue #381)`

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

// downloadedKeyPath matches the .pem path a shell line names a downloaded private key by: the download directory a
// browser writes to, and a name ending in private-key.pem. The line may name it in any position - a "<" redirect,
// scp, cp, mv, cat, install - so the path is matched wherever it stands, and [^\s'"]* keeps the match to one token.
var downloadedKeyPath = regexp.MustCompile(`(?:~/Downloads/[^\s'"]*\.pem|[^\s'"]*private-key\.pem)`)

// checksTheDownloadDirectories matches the line that looks for a leftover key in the download directory and the
// Trash. It names the key without downloading one, so it is no key download of its own. The grep may name the
// suffix or only the stem: a browser saves a second download as "private-key (1).pem", which a check of
// "private-key.pem" misses.
var checksTheDownloadDirectories = regexp.MustCompile(`^\s*(?:sudo\s+)?ls\b.*~/Downloads.*~/\.Trash.*\|\s*grep\s+\S*private-key(?:\.pem)?`)

// boldNumberedStep matches a step of a runbook written as a bold numbered line, "**4. The App's key**: ...". A
// step ends at the next one, so a deletion block past it belongs to that step and not this one (issue #381).
var boldNumberedStep = regexp.MustCompile(`^\*\*\d+\.`)

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

// keyDownload is one line of a page that has the maintainer download a private key: a line of prose that has them
// generate one, or a shell line that feeds a downloaded .pem to a command.
type keyDownload struct {
	line int    // the 1-based line of the download
	path string // the .pem path a shell line names, or "" for a prose download
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

// isDeletionBlock reports whether b is the block that deletes a downloaded key: a shell block that starts with the
// header that says where in the runbook it runs.
func (p keyPage) isDeletionBlock(b keyBlock) bool {
	return b.shell && len(b.body) > 0 && strings.HasPrefix(strings.TrimSpace(b.body[0]), keyDeletionBlockHeader)
}

// downloadedKey returns the .pem path that the shell line at index i (0-based) names as a downloaded private key, or
// "" when it names none. The lines of a deletion block and the check line that greps for a leftover key name the
// key without downloading one, so they count as no download.
func (p keyPage) downloadedKey(i int) string {
	if bi := p.blockAt[i]; bi >= 0 && p.isDeletionBlock(p.blocks[bi]) {
		return ""
	}

	if checksTheDownloadDirectories.MatchString(p.lines[i]) {
		return ""
	}

	return downloadedKeyPath.FindString(p.lines[i])
}

// keyDownloads returns the 1-based lines of the page that have the maintainer download a private key: the prose
// that generates one, and the shell line that names a downloaded .pem as any argument of a command.
func (p keyPage) keyDownloads() []keyDownload {
	var downloads []keyDownload

	for i, line := range p.lines {
		if p.blockAt[i] < 0 {
			if proseDownloadsAKey(line) {
				downloads = append(downloads, keyDownload{line: i + 1})
			}

			continue
		}

		if !p.blocks[p.blockAt[i]].shell {
			continue
		}

		if path := p.downloadedKey(i); path != "" {
			downloads = append(downloads, keyDownload{line: i + 1, path: path})
		}
	}

	return downloads
}

// keySection is one section of a page: the heading that starts it and the lines it spans.
//
// A section runs to the next heading, whatever its level, so a subsection's key downloads are the subsection's own
// and a pin that names the section names the title a maintainer sees (issue #381).
type keySection struct {
	name string // the heading's level and title, "### The AI reviewer"
	from int    // the 1-based line of the heading
	to   int    // the 1-based line of the heading that ends the section, or one past the page
}

// sections returns the sections of the page, in line order.
func (p keyPage) sections() []keySection {
	var sections []keySection

	for i, line := range p.lines {
		if p.blockAt[i] >= 0 {
			continue
		}

		level, title, ok := markdownHeading(line)
		if !ok {
			continue
		}

		if n := len(sections); n > 0 {
			sections[n-1].to = i + 1
		}

		sections = append(sections, keySection{
			name: strings.Repeat("#", level) + " " + title,
			from: i + 1,
			to:   len(p.lines) + 1,
		})
	}

	return sections
}

// keyDownloadsPerSection returns the 1-based lines of the page's key downloads, by the heading of the section that
// holds them. A download before the page's first heading sits under no section, and is gathered under "".
func (p keyPage) keyDownloadsPerSection() map[string][]int {
	sections := p.sections()
	bySection := make(map[string][]int)

	for _, download := range p.keyDownloads() {
		name := ""

		for _, section := range sections {
			if download.line >= section.from && download.line < section.to {
				name = section.name

				break
			}
		}

		bySection[name] = append(bySection[name], download.line)
	}

	return bySection
}

// stepEnd returns the 0-based index of the first line after download (1-based) that ends the step it belongs to,
// or len(p.lines). A step ends at the next heading outside a fenced block, or at the next bold numbered step
// ("**4. The .envrc**"), whichever comes first: a deletion block past the next numbered step belongs to that step,
// and a maintainer who pastes this one's block whole leaves the key behind (issue #381).
//
// A prose download need not be deleted in its own step: the AI reviewer's step 1 downloads the key ("a .pem file
// downloads"), and step 3 is where it is read into the secret and deleted, the only order that leaves no copy
// behind. keyDownloadBlocks settles that case.
func (p keyPage) stepEnd(download int) int {
	for i := download; i < len(p.lines); i++ {
		if p.blockAt[i] >= 0 {
			continue
		}

		if _, _, ok := markdownHeading(p.lines[i]); ok {
			return i
		}

		if boldNumberedStep.MatchString(p.lines[i]) {
			return i
		}
	}

	return len(p.lines)
}

// sameSection reports whether the lines a and b sit under the same heading: no heading outside a fenced block
// stands between them.
func (p keyPage) sameSection(a, b int) bool {
	for i := a; i < b-1; i++ {
		if p.blockAt[i] >= 0 {
			continue
		}

		if _, _, ok := markdownHeading(p.lines[i]); ok {
			return false
		}
	}

	return true
}

// keyDeletionFindings returns every place where the page leaves a downloaded private key behind, in line order:
// one finding per key download whose block of its own is missing or incomplete, and one per comment that only
// describes the deletion.
func (p keyPage) keyDeletionFindings() []keyDeletionFinding {
	downloads := p.keyDownloads()
	servedBy, covered := p.keyDownloadBlocks(downloads)

	var findings []keyDeletionFinding

	for _, download := range downloads {
		text := strings.TrimSpace(p.lines[download.line-1])

		if bi, ok := servedBy[download.line]; ok {
			findings = append(findings, blockFindings(download, text, p.blocks[bi])...)

			continue
		}

		if covered[download.line] {
			continue
		}

		findings = append(findings, keyDeletionFinding{line: download.line, rule: noKeyDeletionBlock, text: text})
	}

	for _, b := range p.blocks {
		if p.isDeletionBlock(b) {
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

// keyDownloadBlocks settles which deletion block serves which key download, as the 1-based line of a download to
// the index of its block, and which prose downloads a later shell download covers.
//
// A shell line that reads a downloaded key needs a block of its own after it, in its own step, so a block serves
// the nearest shell download before it and no other: two downloads in one step need two blocks, and one block after
// both leaves the first key behind (issue #381). A prose download needs a block in its own step, or a later shell
// download in the same section that a block of its own serves - the AI reviewer's step 1, whose key step 3 reads
// and deletes.
func (p keyPage) keyDownloadBlocks(downloads []keyDownload) (servedBy map[int]int, covered map[int]bool) {
	servedBy = make(map[int]int)
	covered = make(map[int]bool)
	taken := make(map[int]bool)

	for bi, b := range p.blocks {
		if !p.isDeletionBlock(b) {
			continue
		}

		for di := len(downloads) - 1; di >= 0; di-- {
			download := downloads[di]
			if download.path == "" || download.line >= b.open || b.open > p.stepEnd(download.line) {
				continue
			}

			if _, ok := servedBy[download.line]; ok {
				continue
			}

			servedBy[download.line] = bi
			taken[bi] = true

			break
		}
	}

	for _, download := range downloads {
		if download.path != "" {
			continue
		}

		if bi, ok := p.deletionBlockForProse(download.line, taken); ok {
			servedBy[download.line] = bi
			taken[bi] = true

			continue
		}

		if p.aLaterShellDownloadDeletes(download, downloads, servedBy) {
			covered[download.line] = true
		}
	}

	return servedBy, covered
}

// deletionBlockForProse returns the index of the first deletion block no download has taken that stands after the
// prose download on line and before the end of its step.
func (p keyPage) deletionBlockForProse(line int, taken map[int]bool) (int, bool) {
	end := p.stepEnd(line)

	for bi, b := range p.blocks {
		if taken[bi] || !p.isDeletionBlock(b) || b.open <= line || b.open > end {
			continue
		}

		return bi, true
	}

	return 0, false
}

// aLaterShellDownloadDeletes reports whether a shell line later in the same section reads a downloaded key and a
// deletion block of its own serves it, so the prose download leaves no key behind.
func (p keyPage) aLaterShellDownloadDeletes(download keyDownload, downloads []keyDownload, servedBy map[int]int) bool {
	for _, later := range downloads {
		if later.path == "" || later.line <= download.line || !p.sameSection(download.line, later.line) {
			continue
		}

		if _, ok := servedBy[later.line]; ok {
			return true
		}
	}

	return false
}

// blockFindings returns what the deletion block b, the one that serves the download, lacks: an rm line of a .pem,
// an ls line of the same path that checks the key is gone, and the line that looks for it in the download directory
// and the Trash. text is the download's own line, which each finding names.
func blockFindings(download keyDownload, text string, b keyBlock) []keyDeletionFinding {
	removed, ok := removesAKey(b.body)
	if !ok {
		return []keyDeletionFinding{{line: download.line, rule: noKeyRemoveLine, text: text}}
	}

	if !checksAKeyIsGone(b.body, removed, download.path) {
		return []keyDeletionFinding{{line: download.line, rule: noKeyGoneCheck, text: text}}
	}

	if !checksTheDownloadDirectoriesForALeftoverKey(b.body) {
		return []keyDeletionFinding{{line: download.line, rule: noKeyTrashCheck, text: text}}
	}

	return nil
}

// removesAKey returns the .pem path the rm line of body removes, and whether body has one.
func removesAKey(body []string) (string, bool) {
	for _, line := range body {
		if m := rmPEM.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1], true
		}
	}

	return "", false
}

// checksAKeyIsGone reports whether body has an ls line of the .pem path removed, with a PASS comment that says the
// file is not there, and whether it names the path a shell download line read, when there is one: a check of
// another .pem, or of the same file under another name, proves nothing about this key.
func checksAKeyIsGone(body []string, removed, read string) bool {
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "# PASS: No such file or directory") {
			continue
		}

		m := lsPEM.FindStringSubmatch(trimmed)
		if m == nil || m[1] != removed || (read != "" && m[1] != read) {
			continue
		}

		return true
	}

	return false
}

// checksTheDownloadDirectoriesForALeftoverKey reports whether body has the line that looks for a leftover
// private-key.pem in the download directory and the Trash, with a PASS comment that says there is none.
func checksTheDownloadDirectoriesForALeftoverKey(body []string) bool {
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		if checksTheDownloadDirectories.MatchString(trimmed) && strings.Contains(trimmed, "# PASS: nothing") {
			return true
		}
	}

	return false
}

// rmPEM and lsPEM match an rm and an ls command that name a .pem path, and capture that path.
var (
	rmPEM = regexp.MustCompile(`^(?:sudo\s+)?rm\s+(?:-\S+\s+)*(\S+\.pem)`)
	lsPEM = regexp.MustCompile(`^(?:sudo\s+)?ls\s+(?:-\S+\s+)*(\S+\.pem)`)
)

// TestEveryKeyDownloadIsFollowedByItsDeletionBlock pins the rule the scan applies to small pages (issue #381): a
// step that downloads a private key needs a block of its own, "# Mac (admin), then:", that removes the .pem,
// checks it is gone and looks for it in the download directory and the Trash; one block serves one download; and a
// comment that only says to delete it is not enough.
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
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"```",
			},
		},
		{
			name: "a deletion block that greps the key's name without its suffix passes",
			page: []string{
				"# Development environment",
				"",
				"**3. Store its secrets:** generate a **new** private key on the App's page.",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				// A browser names a second download "private-key (1).pem", which a check of "private-key.pem"
				// misses, so the check may grep the stem alone.
				"ls ~/Downloads ~/.Trash | grep private-key   # PASS: nothing",
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
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY < ~/Downloads/<app>.<date>.private-key.pem",
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
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY --env ai-review < ~/Downloads/<app>.<date>.private-key.pem",
				"```",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
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
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
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
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"```",
			},
			want: []keyFinding{{line: 1, rule: noKeyRemoveLine}},
		},
		{
			name: "a block that matches the key by another definition of the same name does not check it is gone",
			page: []string{
				"**2. Delete it:** generate a private key on the App's page.",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"```",
			},
			want: []keyFinding{{line: 1, rule: noKeyGoneCheck}},
		},
		{
			name: "a block that deletes another key than the shell line read does not check this one is gone",
			page: []string{
				"# Development environment",
				"",
				"**3. Store the key:**",
				"",
				"```bash",
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY < ~/Downloads/<app>.<date>.private-key.pem",
				"```",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<other>.<date>.private-key.pem",
				"ls ~/Downloads/<other>.<date>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"```",
			},
			want: []keyFinding{{line: 6, rule: noKeyGoneCheck}},
		},
		{
			name: "a block that does not look for the key in the Trash fails",
			page: []string{
				"**2. Delete it:** generate a private key on the App's page.",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				"```",
			},
			want: []keyFinding{{line: 1, rule: noKeyTrashCheck}},
		},
		{
			name: "a block under another comment is not the deletion block",
			page: []string{
				"# Development environment",
				"",
				"**3. Store the key:** generate a new private key on the App's page.",
				"",
				"```bash",
				"# Mac, host clone",
				"cat ~/Downloads/<app>.<date>.private-key.pem > ~/agents-app.pem",
				"```",
			},
			want: []keyFinding{
				{line: 3, rule: noKeyDeletionBlock},
				{line: 7, rule: noKeyDeletionBlock},
			},
		},
		{
			name: "a block past the next numbered step belongs to that step",
			page: []string{
				"# Development environment",
				"",
				"**4. The App's key**: generate a **new** private key on the App's settings page.",
				"",
				"```bash",
				"ssh <the Mini> 'umask 077 && cat >~/agents-app.pem.tmp' < ~/Downloads/<app>.<date>.private-key.pem",
				"```",
				"",
				"**5. The account's SSH key to the VM:**",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"```",
			},
			want: []keyFinding{
				{line: 3, rule: noKeyDeletionBlock},
				{line: 6, rule: noKeyDeletionBlock},
			},
		},
		{
			name: "a step that downloads the key twice needs two deletion blocks",
			page: []string{
				"# Development environment",
				"",
				"**3. Store the key:**",
				"",
				"```bash",
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY < ~/Downloads/<app>.<date>.private-key.pem",
				"gh secret set AI_REVIEWER_APP_PRIVATE_KEY_FALLBACK < ~/Downloads/<app>.<date>.private-key.pem",
				"```",
				"",
				"```bash",
				"# Mac (admin), then:",
				"rm ~/Downloads/<app>.<date>.private-key.pem",
				"ls ~/Downloads/<app>.<date>.private-key.pem   # PASS: No such file or directory",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"```",
			},
			want: []keyFinding{{line: 6, rule: noKeyDeletionBlock}},
		},
		{
			name: "an scp of the downloaded key is a download that needs its deletion block",
			page: []string{
				"# Development environment",
				"",
				"```bash",
				"scp ~/Downloads/<app>.<date>.private-key.pem <the Mini>:/tmp/",
				"```",
			},
			want: []keyFinding{{line: 4, rule: noKeyDeletionBlock}},
		},
		{
			name: "a move of the downloaded key is a download that needs its deletion block",
			page: []string{
				"# Development environment",
				"",
				"```bash",
				"mv ~/Downloads/<app>.<date>.private-key.pem ~/agents-app.pem",
				"```",
			},
			want: []keyFinding{{line: 4, rule: noKeyDeletionBlock}},
		},
		{
			name: "the ls that greps for a leftover key and the block's own lines are no downloads",
			page: []string{
				"# Development environment",
				"",
				"```bash",
				"ls ~/Downloads ~/.Trash | grep private-key.pem   # PASS: nothing",
				"ls ~/Downloads ~/.Trash | grep private-key   # PASS: nothing",
				"ls ~/agents-app.pem.tmp   # PASS: No such file or directory",
				"```",
			},
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
// by a block of its own that deletes the .pem, checks it is gone and looks for it in the download directory and the
// Trash, and no comment only says to delete it.
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

// TestDevEnvironmentKeyDownloadsPerSection pins where on the page the scan finds the key downloads it requires a
// deletion block for, so a reworded step that stops matching fails here instead of quietly passing the rule with one
// download fewer (issue #381). The pin is per section, not per line: a step may move inside its section, and a new
// download step in a section of its own has to be pinned on purpose.
func TestDevEnvironmentKeyDownloadsPerSection(t *testing.T) {
	t.Parallel()

	// The key downloads of each section that has any, and the count the page is expected to hold. A download the pin
	// does not name, or a section whose count moved, fails the test.
	want := map[string]int{
		"### The agents App":                    2,
		"#### The key under a separate account": 4,
		"### The AI reviewer":                   2,
	}

	data, err := fs.ReadFile(os.DirFS(repoRoot), devEnvironmentPage)
	if err != nil {
		t.Fatalf("reading %s: %v", devEnvironmentPage, err)
	}

	got := newKeyPage(string(data)).keyDownloadsPerSection()

	for _, name := range slices.Sorted(maps.Keys(want)) {
		if len(got[name]) == want[name] {
			continue
		}

		t.Errorf("%s: section %q has %d key download(s) on line(s) %v, the test pins %d: a download step was added, removed, moved to another section or stopped being matched. Give a new download step its deletion block, then update this pin to the section's count (issue #381)",
			devEnvironmentPage, name, len(got[name]), got[name], want[name])
	}

	for _, name := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[name]; ok {
			continue
		}

		t.Errorf("%s: section %q is not pinned, but holds %d key download(s) on line(s) %v: give each download its deletion block, then add the section to this pin with that count (issue #381)",
			devEnvironmentPage, name, len(got[name]), got[name])
	}
}
