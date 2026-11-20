package review

import (
	"slices"
	"strings"
)

// LeftOutFile is one file the review left out of the diff it read (issues
// #324, #342).
type LeftOutFile struct {
	// Path is the path as git writes it in the section's header.
	Path string
	// Added is how many lines the file's section adds: 0 for a deleted or a
	// binary file and for a rename's old path.
	Added int
	// Binary is true for a file git writes as binary ("Binary files ...
	// differ" or "GIT binary patch"): it has no line of text, so it was
	// neither reviewed nor scanned (issue #342).
	Binary bool
	// Renamed is true for the old path of a rename or copy that no section
	// names as its new path: it is the same file as the new one, whose lines
	// are counted there.
	Renamed bool
}

// ScanKind names what the model-free scan of the left-out files found.
type ScanKind string

// The kinds of value the scan reports (issue #342).
const (
	ScanAccountID ScanKind = "12-digit account ID"
	ScanIPv4      ScanKind = "IPv4 address"
	ScanHostname  ScanKind = "host name"
	ScanKey       ScanKind = "key-like string"
)

// ScanFinding is one finding of the scan: the file, the line of its new side
// and the kind of value found there. It never holds the matched value: the
// review is public, and the value is what must not be published.
type ScanFinding struct {
	Path string
	Line int
	Kind ScanKind
}

// LeftOut is what the reviewer left out of the diff it read: each file with
// the lines it adds, and what the scan found in those lines.
type LeftOut struct {
	Files    []LeftOutFile
	Findings []ScanFinding
}

// MaxScanFindings is how many of the scan's findings a review lists; it counts
// the rest, so a pull request cannot grow the review without bound.
const MaxScanFindings = 20

// ScanLeftOut returns the files FilterDiff leaves out of raw, path for path and
// in its sorted order, each with the lines its section adds, and every finding
// of a scan of those added lines alone, in the diff's order (issue #342). A
// removed or a context line is no new data, and a reviewed file is the model's
// to read, so neither is scanned. The scan is a fixed set of rules (scanLine)
// with no model: it reads a left-out section's hunks, never its header, and
// keeps the kind and the line of what it found, never the text. It does not cap
// the findings: the review caps what it lists. A binary file is marked Binary
// and a rename's old path Renamed, so the note never shows either as a file
// nothing was added to. The left-out paths themselves are shown as they are,
// escaped and cut, and are not scanned: the pull request's own file list
// already shows them.
func ScanLeftOut(raw string) LeftOut {
	files := map[string]*LeftOutFile{}
	asNew := map[string]bool{}

	var (
		leftOut LeftOut
		section strings.Builder
	)

	file := func(path string) *LeftOutFile {
		if files[path] == nil {
			files[path] = &LeftOutFile{Path: path}
		}

		return files[path]
	}

	flush := func() {
		text := section.String()
		section.Reset()

		paths, ok := leftOutPaths(text)
		if !ok {
			return
		}

		// Both paths of a rename or copy are left out; the lines it adds
		// belong to the new one, and the old one is marked renamed unless a
		// section of its own names it as its new path.
		oldPath, newPath := paths[0], paths[len(paths)-1]
		asNew[newPath] = true

		if oldPath != newPath {
			file(oldPath).Renamed = true
		}

		count, findings := scanSection(text, newPath)

		target := file(newPath)
		target.Added += count
		target.Binary = target.Binary || isBinarySection(text)
		leftOut.Findings = append(leftOut.Findings, findings...)
	}

	for line := range strings.SplitAfterSeq(raw, "\n") {
		if strings.HasPrefix(line, diffHeader) {
			flush()
		}

		section.WriteString(line)
	}

	flush()

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}

	slices.Sort(paths)

	for _, path := range paths {
		f := *files[path]
		f.Renamed = f.Renamed && !asNew[path]
		leftOut.Files = append(leftOut.Files, f)
	}

	return leftOut
}

// isBinarySection reports whether git writes a file section as binary: a
// "Binary files ... differ" line, or a "GIT binary patch" as "git diff
// --binary" writes. Neither line can be a hunk's, which starts with " ", "+",
// "-" or "\".
func isBinarySection(section string) bool {
	for line := range strings.SplitSeq(section, "\n") {
		if line == "GIT binary patch" ||
			(strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ")) {
			return true
		}
	}

	return false
}

// scanSection counts the lines a file section's hunks add and scans each, by
// the line of the file's new side it is. The hunk headers' counts say where a
// hunk ends, as in newSideLines, so a line of content never reads as a header.
func scanSection(section, path string) (int, []ScanFinding) {
	var (
		count            int
		findings         []ScanFinding
		newLine          int
		oldLeft, newLeft int
	)

	for text := range strings.SplitSeq(section, "\n") {
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(text, "+"):
				count++

				for _, kind := range scanLine(text[1:]) {
					findings = append(findings, ScanFinding{Path: path, Line: newLine, Kind: kind})
				}

				newLine++
				newLeft--
			case strings.HasPrefix(text, "-"):
				oldLeft--
			case strings.HasPrefix(text, `\`):
				// "\ No newline at end of file": no line of either side.
			default:
				newLine++
				newLeft--
				oldLeft--
			}

			continue
		}

		if strings.HasPrefix(text, "@@ ") {
			newLine, oldLeft, newLeft = parseHunkHeader(text)
		}
	}

	return count, findings
}

// scanLine returns the kinds of value line holds, each once, in a fixed order.
// Each rule reads maximal runs of one character class, so a value is matched
// whole or not at all, and each knows the sanitised values CLAUDE.md rule 2
// asks the fixtures for:
//
//   - an account ID is a run of exactly twelve digits that is not 123456789012,
//     has no letter, digit, "-" or "_" beside it (a UUID's last group is no
//     account ID), has no "." or "/" beside it with a digit on the far side (a
//     decimal number), and is not inside a JSON pointer starting "#/";
//   - an IPv4 address is four dotted decimal octets with no letter or digit
//     beside them, outside 0.0.0.0/8, 127.0.0.0/8, the documentation ranges
//     192.0.2.0/24, 198.51.100.0/24 and 203.0.113.0/24, and 255.255.255.255;
//   - a host name is dotted labels whose last is a public top-level domain of
//     a fixed list, so a taxonomy category such as compute.vm is none, and
//     that is neither an example domain at any depth (exampleDomains) nor a
//     provider's domain (providerDomains) as the apex, with exactly one label
//     below it, or for amazonaws.com <service>.<region> with a region of the
//     AWS shape: anything deeper is a name a customer chose;
//   - a key is a run of letters and digits that is an access key ID (twenty
//     upper-case letters and digits starting AKIA or ASIA), or a run of
//     letters, digits, "+" and "/" that begins and ends with a letter or a
//     digit, is at least 32 long and holds an upper-case letter, a lower-case
//     letter and a digit (a secret access key); a run that begins or ends with
//     "/" is a path or a JSON pointer, and "-" ends a run.
func scanLine(line string) []ScanKind {
	var kinds []ScanKind

	for _, rule := range []struct {
		kind  ScanKind
		found func(string) bool
	}{
		{ScanAccountID, hasAccountID},
		{ScanIPv4, hasIPv4},
		{ScanHostname, hasHostname},
		{ScanKey, hasKey},
	} {
		if rule.found(line) {
			kinds = append(kinds, rule.kind)
		}
	}

	return kinds
}

// documentationAccountID is the account ID CLAUDE.md rule 2 asks the fixtures for.
const documentationAccountID = "123456789012"

// hasAccountID reports whether line holds an account ID (scanLine).
func hasAccountID(line string) bool {
	return anyRunAt(line, isDecimalDigit, func(start, end int) bool {
		if end-start != 12 || line[start:end] == documentationAccountID {
			return false
		}

		var before, beyondBefore, after, beyondAfter byte

		if start > 0 {
			before = line[start-1]
		}

		if start > 1 {
			beyondBefore = line[start-2]
		}

		if end < len(line) {
			after = line[end]
		}

		if end+1 < len(line) {
			beyondAfter = line[end+1]
		}

		return !isIDNeighbour(before, beyondBefore) && !isIDNeighbour(after, beyondAfter) &&
			!inJSONPointer(line, start)
	})
}

// isIDNeighbour reports whether c, beside a run of digits with beyond on the
// far side of c, makes it part of a longer identifier rather than a value of
// its own: a letter, a digit, "-" or "_", or a "." or "/" with a digit beyond,
// as in a decimal number.
func isIDNeighbour(c, beyond byte) bool {
	switch c {
	case '-', '_':
		return true
	case '.', '/':
		return isDecimalDigit(beyond)
	default:
		return isASCIIAlnum(c)
	}
}

// inJSONPointer reports whether the word of line that holds the byte at i, the
// text after the last space or quote before it, is a JSON pointer: one that
// starts "#/" is a reference into the document, never a value.
func inJSONPointer(line string, i int) bool {
	word := line[strings.LastIndexAny(line[:i], " \t\"'`")+1 : i]

	return strings.HasPrefix(word, "#/")
}

// hasIPv4 reports whether line holds an IPv4 address (scanLine).
func hasIPv4(line string) bool {
	return anyRun(line, func(c byte) bool { return isDecimalDigit(c) || c == '.' },
		func(run string, before, after byte) bool {
			if isASCIIAlnum(before) || isASCIIAlnum(after) {
				return false
			}

			octets, ok := parseIPv4(strings.TrimRight(run, "."))

			return ok && !isReservedIPv4(octets)
		})
}

// parseIPv4 reads four dotted decimal octets of one to three digits each.
func parseIPv4(s string) ([4]int, bool) {
	var octets [4]int

	parts := strings.Split(s, ".")
	if len(parts) != len(octets) {
		return octets, false
	}

	for i, part := range parts {
		if part == "" || len(part) > 3 {
			return octets, false
		}

		for j := range len(part) {
			octets[i] = octets[i]*10 + int(part[j]-'0')
		}

		if octets[i] > 255 {
			return octets, false
		}
	}

	return octets, true
}

// isReservedIPv4 reports whether an address is one no real network is reached
// at, or one documentation uses (scanLine).
func isReservedIPv4(o [4]int) bool {
	switch {
	case o[0] == 0, o[0] == 127:
		return true
	case o[0] == 192 && o[1] == 0 && o[2] == 2,
		o[0] == 198 && o[1] == 51 && o[2] == 100,
		o[0] == 203 && o[1] == 0 && o[2] == 113:
		return true
	default:
		return o == [4]int{255, 255, 255, 255}
	}
}

// publicTLDs are the top-level domains a host name ends in for the scan: a
// fixed list of common ones, which no taxonomy category, file extension or
// JSON field of this repository ends in.
var publicTLDs = []string{
	"ai", "app", "at", "be", "biz", "ch", "cloud", "co", "com", "de", "dev", "dk", "es", "eu", "fi",
	"fr", "ie", "info", "io", "net", "nl", "online", "org", "pl", "se", "site", "tech", "uk", "us", "xyz",
}

// exampleDomains are the example domains of RFC 2606: the sanitised values
// CLAUDE.md rule 2 asks the fixtures for, so no name below them is a finding.
var exampleDomains = []string{"example.com", "example.net", "example.org"}

// providerDomains are the public domains of the sources, the targets and the
// tools this repository names. Their apex and a name exactly one label below it
// (a service's endpoint) are no finding; a deeper name is one a customer chose
// (a bucket, a database, a registry), the value most likely to be real.
var providerDomains = []string{
	"amazon.com", "amazonaws.com", "azure.com", "azure.net", "microsoft.com", "windows.net",
	"google.com", "googleapis.com",
	"ovh.com", "ovh.net", "ovhcloud.com", "scaleway.com", "scw.cloud", "upcloud.com",
	"github.com", "githubusercontent.com", "ghcr.io", "go.dev", "golang.org",
}

// isAllowedHost reports whether host, lower-case, is an example domain's name
// or a provider's own endpoint (scanLine).
func isAllowedHost(host string) bool {
	if slices.ContainsFunc(exampleDomains, func(domain string) bool {
		return host == domain || strings.HasSuffix(host, "."+domain)
	}) {
		return true
	}

	return slices.ContainsFunc(providerDomains, func(domain string) bool {
		if host == domain {
			return true
		}

		below, ok := strings.CutSuffix(host, "."+domain)
		if !ok {
			return false
		}

		labels := strings.Split(below, ".")

		return len(labels) == 1 ||
			(domain == "amazonaws.com" && len(labels) == 2 && isAWSRegion(labels[1]))
	})
}

// isAWSRegion reports whether label has the shape of an AWS region: two
// letters, one or more words of letters and a number, joined by "-"
// (eu-west-1, us-gov-east-1, ap-southeast-2).
func isAWSRegion(label string) bool {
	parts := strings.Split(label, "-")
	if len(parts) < 3 || len(parts[0]) != 2 {
		return false
	}

	for _, part := range parts[:len(parts)-1] {
		if part == "" || strings.ContainsFunc(part, func(r rune) bool { return r < 'a' || r > 'z' }) {
			return false
		}
	}

	last := parts[len(parts)-1]

	return last != "" && !strings.ContainsFunc(last, func(r rune) bool { return r < '0' || r > '9' })
}

// hasHostname reports whether line holds a host name (scanLine).
func hasHostname(line string) bool {
	return anyRun(line, func(c byte) bool { return isASCIIAlnum(c) || c == '.' || c == '-' },
		func(run string, _, _ byte) bool {
			host := strings.ToLower(strings.Trim(run, ".-"))

			labels := strings.Split(host, ".")
			if len(labels) < 2 || !slices.Contains(publicTLDs, labels[len(labels)-1]) {
				return false
			}

			for _, label := range labels {
				if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
					return false
				}
			}

			return !isAllowedHost(host)
		})
}

// minTokenLength is the shortest run of letters and digits the scan takes for
// a token, given an upper-case letter, a lower-case letter and a digit.
const minTokenLength = 32

// hasKey reports whether line holds a key-like string (scanLine).
func hasKey(line string) bool {
	alnumKey := anyRun(line, isASCIIAlnum, func(run string, _, _ byte) bool {
		return isAccessKeyID(run) || isToken(run)
	})

	return alnumKey || anyRun(line, isBase64Char, func(run string, _, _ byte) bool {
		return isASCIIAlnum(run[0]) && isASCIIAlnum(run[len(run)-1]) && isToken(run)
	})
}

// isAccessKeyID reports whether run is an access key ID: twenty upper-case
// letters and digits starting AKIA or ASIA.
func isAccessKeyID(run string) bool {
	return len(run) == 20 && (strings.HasPrefix(run, "AKIA") || strings.HasPrefix(run, "ASIA")) &&
		!strings.ContainsFunc(run, func(r rune) bool { return r >= 'a' && r <= 'z' })
}

// isToken reports whether run is at least minTokenLength long and holds an
// upper-case letter, a lower-case letter and a digit.
func isToken(run string) bool {
	return len(run) >= minTokenLength &&
		strings.ContainsFunc(run, func(r rune) bool { return r >= 'A' && r <= 'Z' }) &&
		strings.ContainsFunc(run, func(r rune) bool { return r >= 'a' && r <= 'z' }) &&
		strings.ContainsFunc(run, func(r rune) bool { return r >= '0' && r <= '9' })
}

// isBase64Char reports whether c is in the alphabet of a secret access key:
// a letter, a digit, "+" or "/".
func isBase64Char(c byte) bool {
	return isASCIIAlnum(c) || c == '+' || c == '/'
}

// anyRun reports whether match holds for any maximal run of the bytes of s in
// class, given the run and the bytes before and after it (0 at either end of s).
func anyRun(s string, class func(byte) bool, match func(run string, before, after byte) bool) bool {
	return anyRunAt(s, class, func(start, end int) bool {
		var before, after byte

		if start > 0 {
			before = s[start-1]
		}

		if end < len(s) {
			after = s[end]
		}

		return match(s[start:end], before, after)
	})
}

// anyRunAt reports whether match holds for any maximal run of the bytes of s
// in class, given the run's start and end in s.
func anyRunAt(s string, class func(byte) bool, match func(start, end int) bool) bool {
	for start := 0; start < len(s); {
		if !class(s[start]) {
			start++

			continue
		}

		end := start
		for end < len(s) && class(s[end]) {
			end++
		}

		if match(start, end) {
			return true
		}

		start = end
	}

	return false
}
