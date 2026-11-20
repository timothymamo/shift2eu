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
	// binary file.
	Added int
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
// the findings: the review caps what it lists.
func ScanLeftOut(raw string) LeftOut {
	added := map[string]int{}

	var (
		leftOut LeftOut
		section strings.Builder
	)

	flush := func() {
		text := section.String()
		section.Reset()

		paths, ok := leftOutPaths(text)
		if !ok {
			return
		}

		newPath := paths[len(paths)-1]

		// Both paths of a rename or copy are left out; the lines it adds
		// belong to the new one.
		for _, path := range paths {
			if _, seen := added[path]; !seen {
				added[path] = 0
			}
		}

		count, findings := scanSection(text, newPath)
		added[newPath] += count
		leftOut.Findings = append(leftOut.Findings, findings...)
	}

	for line := range strings.SplitAfterSeq(raw, "\n") {
		if strings.HasPrefix(line, diffHeader) {
			flush()
		}

		section.WriteString(line)
	}

	flush()

	paths := make([]string, 0, len(added))
	for path := range added {
		paths = append(paths, path)
	}

	slices.Sort(paths)

	for _, path := range paths {
		leftOut.Files = append(leftOut.Files, LeftOutFile{Path: path, Added: added[path]})
	}

	return leftOut
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
//   - an account ID is a run of exactly twelve digits that is not 123456789012
//     and has no letter, digit, "-", "_", "." or "/" beside it (a UUID's last
//     group is no account ID);
//   - an IPv4 address is four dotted decimal octets with no letter or digit
//     beside them, outside 0.0.0.0/8, 127.0.0.0/8, the documentation ranges
//     192.0.2.0/24, 198.51.100.0/24 and 203.0.113.0/24, and 255.255.255.255;
//   - a host name is dotted labels whose last is a public top-level domain of
//     a fixed list, outside the example domains and the domains of the
//     providers this project compares (allowedDomains), so a taxonomy category
//     such as compute.vm is none;
//   - a key is a run of letters and digits that is an access key ID (twenty
//     upper-case letters and digits starting AKIA or ASIA) or at least 32 long
//     with an upper-case letter, a lower-case letter and a digit; "/" and "-"
//     end a run, so a JSON pointer is none.
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
	return anyRun(line, isDecimalDigit, func(run string, before, after byte) bool {
		return len(run) == 12 && run != documentationAccountID &&
			!isIDNeighbour(before) && !isIDNeighbour(after)
	})
}

// isIDNeighbour reports whether c, beside a run of digits, makes it part of a
// longer identifier rather than a value of its own.
func isIDNeighbour(c byte) bool {
	return isASCIIAlnum(c) || c == '-' || c == '_' || c == '.' || c == '/'
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

// allowedDomains are the domains whose host names are no finding, with every
// name below them: the example domains of RFC 2606 and the public domains of
// the sources, the targets and the tools this repository names.
var allowedDomains = []string{
	"example.com", "example.net", "example.org",
	"amazon.com", "amazonaws.com", "azure.com", "azure.net", "microsoft.com", "windows.net",
	"google.com", "googleapis.com",
	"ovh.com", "ovh.net", "ovhcloud.com", "scaleway.com", "scw.cloud", "upcloud.com",
	"github.com", "githubusercontent.com", "ghcr.io", "go.dev", "golang.org",
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

			return !slices.ContainsFunc(allowedDomains, func(domain string) bool {
				return host == domain || strings.HasSuffix(host, "."+domain)
			})
		})
}

// minTokenLength is the shortest run of letters and digits the scan takes for
// a token, given an upper-case letter, a lower-case letter and a digit.
const minTokenLength = 32

// hasKey reports whether line holds a key-like string (scanLine).
func hasKey(line string) bool {
	return anyRun(line, isASCIIAlnum, func(run string, _, _ byte) bool {
		if len(run) == 20 && (strings.HasPrefix(run, "AKIA") || strings.HasPrefix(run, "ASIA")) &&
			!strings.ContainsFunc(run, func(r rune) bool { return r >= 'a' && r <= 'z' }) {
			return true
		}

		return len(run) >= minTokenLength &&
			strings.ContainsFunc(run, func(r rune) bool { return r >= 'A' && r <= 'Z' }) &&
			strings.ContainsFunc(run, func(r rune) bool { return r >= 'a' && r <= 'z' }) &&
			strings.ContainsFunc(run, func(r rune) bool { return r >= '0' && r <= '9' })
	})
}

// anyRun reports whether match holds for any maximal run of the bytes of s in
// class, given the run and the bytes before and after it (0 at either end of s).
func anyRun(s string, class func(byte) bool, match func(run string, before, after byte) bool) bool {
	for start := 0; start < len(s); {
		if !class(s[start]) {
			start++

			continue
		}

		end := start
		for end < len(s) && class(s[end]) {
			end++
		}

		var before, after byte

		if start > 0 {
			before = s[start-1]
		}

		if end < len(s) {
			after = s[end]
		}

		if match(s[start:end], before, after) {
			return true
		}

		start = end
	}

	return false
}
