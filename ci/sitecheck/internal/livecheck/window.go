package livecheck

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window is the scheduled check's version window (PRD #297 milestone 4,
// ADR 0043): while a release is on its way, the live site may still show the
// release immediately before the highest tag.
type Window struct {
	// Anchor is the committer date of the commit the highest release tag
	// points to (git log -1 --format=%cI <tag>).
	Anchor time.Time
	// Bound is the window's fixed length after Anchor; it must be positive.
	Bound time.Duration
	// Now is the clock the window is judged at.
	Now time.Time
}

// AcceptLive judges live, the version the live page showed, matched against
// tags with MatchVersion. The highest tag of tags (by version, never
// lexically; entries that are not v<major>.<minor>.<patch> are ignored) is
// always accepted. The tag immediately below it is accepted only while w.Now is
// before w.Anchor+w.Bound. Everything else is refused with an error: the zero
// Tag, a live tag that is not an entry of tags, an empty tag list, a
// non-positive bound, and any other tag. The error names only tags from the
// tag list, never text of a response.
func AcceptLive(live Tag, tags []string, w Window) error {
	if live.IsZero() {
		return errZeroTag
	}

	if w.Bound <= 0 {
		return fmt.Errorf("the version window's bound %v is not positive", w.Bound)
	}

	liveVersion, ok := parseVersion(live.tag)
	if !ok {
		return fmt.Errorf("the live tag %s is not v<major>.<minor>.<patch>", Quote(live.tag))
	}

	versions := make([]version, 0, len(tags))
	found := false

	for _, tag := range tags {
		v, ok := parseVersion(tag)
		if !ok {
			continue
		}

		versions = append(versions, v)

		if tag == live.tag {
			found = true
		}
	}

	if len(versions) == 0 {
		return errNoReleaseTag
	}

	if !found {
		return fmt.Errorf("the live version %s is no release tag of the list", live.tag)
	}

	highest := versions[0]
	for _, v := range versions[1:] {
		if highest.less(v) {
			highest = v
		}
	}

	if liveVersion == highest {
		return nil
	}

	var below version

	hasBelow := false

	for _, v := range versions {
		if v.less(highest) && (!hasBelow || below.less(v)) {
			below, hasBelow = v, true
		}
	}

	if !hasBelow || liveVersion != below {
		return fmt.Errorf("the live version %s is neither the highest release tag %s nor the one immediately below it",
			live.tag, highest)
	}

	end := w.Anchor.Add(w.Bound)
	if !w.Now.Before(end) {
		return fmt.Errorf("the live version %s is the release before the highest tag %s, and the window ended at %s",
			live.tag, highest, end.UTC().Format(time.RFC3339))
	}

	return nil
}

// errNoReleaseTag is AcceptLive's error for a tag list with no release tag.
var errNoReleaseTag = errors.New("the tag list holds no release tag v<major>.<minor>.<patch>")

// version is a release tag's three numbers.
type version struct {
	major, minor, patch uint64
}

// String returns the version as its tag, "v<major>.<minor>.<patch>".
func (v version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
}

// less reports whether v is a lower version than o.
func (v version) less(o version) bool {
	if v.major != o.major {
		return v.major < o.major
	}

	if v.minor != o.minor {
		return v.minor < o.minor
	}

	return v.patch < o.patch
}

// parseVersion parses a tag of the form ^v[0-9]+\.[0-9]+\.[0-9]+$. A tag of
// any other form, or one whose numbers overflow, is not a version.
func parseVersion(tag string) (version, bool) {
	if !versionPattern.MatchString(tag) {
		return version{}, false
	}

	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")

	var nums [3]uint64

	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version{}, false
		}

		nums[i] = n
	}

	return version{major: nums[0], minor: nums[1], patch: nums[2]}, true
}
