package livecheck

import "io"

// MaxTagListBytes caps how much of the tag list ReadTagList reads: a longer
// input is an error.
const MaxTagListBytes = 1 << 20

// Repository is the GitHub repository whose releases the commands name.
const Repository = "timothymamo/shift2eu"

// SignerWorkflow is the workflow whose attestation a release's site bundle
// must carry, as gh attestation verify's --signer-workflow takes it.
const SignerWorkflow = Repository + "/.github/workflows/release.yml"

// Tag is a release tag of the repository that the live version matched. Its
// zero value is no tag. Only MatchVersion constructs one, and it holds the
// string taken from the tag list, never the page's text, so every command the
// package builds from it is built from the repository's own tag (behaviour 9).
type Tag struct {
	tag string
}

// String returns the tag ("v1.2.3"), or "" for the zero Tag.
func (t Tag) String() string {
	return t.tag
}

// IsZero reports whether t is the zero Tag, which MatchVersion never returns
// with a nil error.
func (t Tag) IsZero() bool {
	return t.tag == ""
}

// ReadTagList reads the repository's tag list from r, one tag per line as
// `git tag --list` prints it, at most MaxTagListBytes (more is an error). A
// trailing "\r" of a line is removed and empty lines are skipped; no other
// trimming is done. The tags are returned in the order of the input.
func ReadTagList(r io.Reader) ([]string, error) {
	return nil, stub(r)
}

// MatchVersion matches text, the version read from the live page (untrusted),
// against tags. text must match ^v[0-9]+\.[0-9]+\.[0-9]+$ exactly, with no
// surrounding white space or line break, and be at most MaxVersionBytes long;
// and some entry of tags must equal it byte for byte. It returns the Tag
// holding that entry of tags. An empty text, a text that does not match, or
// one no tag equals is an error, whose message quotes text with Quote.
func MatchVersion(text string, tags []string) (Tag, error) {
	return Tag{}, stub(text, tags)
}

// BundleName returns the file name of the release's site bundle,
// "shift2eu-site_<tag>.tar.gz" (ci/sitebuild's archive, as the Makefile names
// it). The zero Tag is an error.
func BundleName(t Tag) (string, error) {
	return "", stub(t)
}

// DownloadArgv returns the argv of the download of the release's site bundle
// into dir:
//
//	gh release download <tag> --repo <Repository> --pattern <BundleName(t)> --dir <dir>
//
// one argument per element, never a shell string. dir is the caller's own
// path, never text of a response; an empty dir or one starting with "-" is an
// error, as is the zero Tag.
func DownloadArgv(t Tag, dir string) ([]string, error) {
	return nil, stub(t, dir)
}

// AttestationVerifyArgv returns the argv of the attestation check of the
// downloaded bundle at bundlePath:
//
//	gh attestation verify <bundlePath> --repo <Repository> --signer-workflow <SignerWorkflow> --source-ref refs/tags/<tag> --deny-self-hosted-runners
//
// one argument per element, never a shell string. bundlePath is the caller's
// own path; an empty one or one starting with "-" is an error, as is the zero
// Tag.
func AttestationVerifyArgv(t Tag, bundlePath string) ([]string, error) {
	return nil, stub(t, bundlePath)
}
