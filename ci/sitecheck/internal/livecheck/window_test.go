package livecheck_test

import (
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the scheduled check's version window (PRD #297 milestone 4,
// ADR 0043): a live version lower than the highest release tag on main is
// accepted only for the release immediately before it, and only while the
// clock is before the anchor taken from the highest tag's commit plus the
// window's fixed bound. Everything else is refused: a version two releases
// behind, a version higher than the highest tag, the zero Tag, an empty tag
// list, and a bound that is not positive.
//
// The tags are compared by version, never lexically: git's own order puts
// v0.9.0 after v0.10.0, and a comparison that read the strings would take the
// wrong release for the highest one.

// windowBound is the bound the cases use unless they say otherwise: long
// enough for a release, short enough that a clock a little after the anchor
// leaves the window.
const windowBound = 6 * time.Hour

// windowAnchor is the committer date of the commit the highest release tag
// points to, as the workflow reads it with git log -1 --format=%cI.
var windowAnchor = time.Date(2026, time.March, 1, 8, 0, 0, 0, time.UTC)

// windowReleaseTags is a release tag list in the order `git tag --list` prints
// it. Its highest tag is v0.16.1 and the one immediately below it v0.16.0.
var windowReleaseTags = []string{"v0.15.0", "v0.16.0", "v0.16.1"}

// windowTag matches text, the version a live page showed, against tags with
// MatchVersion: the cases build the live Tag only the way the check does, so
// the value AcceptLive judges is always the tag list's own entry.
func windowTag(t *testing.T, tags []string, text string) livecheck.Tag {
	t.Helper()

	tag, err := livecheck.MatchVersion(text, tags)
	if err != nil {
		t.Fatalf("MatchVersion(%q, %v) failed: %v: the case builds the live Tag from the tag list", text, tags, err)
	}

	return tag
}

// TestAcceptLive pins the window: the highest tag is always accepted, the tag
// immediately below it only while Now is before Anchor+Bound, and everything
// else is refused with an error.
func TestAcceptLive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// text is the version the page showed, matched against matchTags.
		text string
		// matchTags is the tag list MatchVersion matched text against.
		matchTags []string
		// judged is the tag list AcceptLive judges against; nil means
		// matchTags, and an empty non-nil slice is an empty tag list.
		judged []string
		// zeroTag judges the zero Tag, which no match returns.
		zeroTag bool
		// bound is the window's fixed bound.
		bound time.Duration
		// at is how long after windowAnchor the clock reads.
		at time.Duration
		// wantErr is whether AcceptLive must refuse the live version.
		wantErr bool
	}{
		{
			name:      "the highest tag, long after the window",
			text:      "v0.16.1",
			matchTags: windowReleaseTags,
			bound:     windowBound,
			at:        1000 * time.Hour,
		},
		{
			name:      "the tag immediately below, inside the window",
			text:      "v0.16.0",
			matchTags: windowReleaseTags,
			bound:     windowBound,
			at:        time.Hour,
		},
		{
			name:      "the tag immediately below, at the end of the window",
			text:      "v0.16.0",
			matchTags: windowReleaseTags,
			bound:     windowBound,
			at:        windowBound,
			wantErr:   true,
		},
		{
			name:      "the tag immediately below, after the window",
			text:      "v0.16.0",
			matchTags: windowReleaseTags,
			bound:     windowBound,
			at:        windowBound + time.Second,
			wantErr:   true,
		},
		{
			name:      "two tags below the highest, inside the window",
			text:      "v0.15.0",
			matchTags: windowReleaseTags,
			bound:     windowBound,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			name:      "the zero Tag",
			zeroTag:   true,
			matchTags: windowReleaseTags,
			bound:     windowBound,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			name:      "an empty tag list",
			text:      "v0.16.1",
			matchTags: windowReleaseTags,
			judged:    []string{},
			bound:     windowBound,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			name:      "a bound of zero",
			text:      "v0.16.0",
			matchTags: windowReleaseTags,
			bound:     0,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			name:      "a negative bound",
			text:      "v0.16.0",
			matchTags: windowReleaseTags,
			bound:     -time.Hour,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			name:      "one tag, and the live version is it",
			text:      "v0.16.1",
			matchTags: []string{"v0.16.1"},
			bound:     windowBound,
			at:        1000 * time.Hour,
		},
		{
			name:      "the tags judged are a smaller list than the one the version matched",
			text:      "v0.16.1",
			matchTags: windowReleaseTags,
			judged:    []string{"v0.15.0", "v0.16.0"},
			bound:     windowBound,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			// A live version higher than the highest tag is refused. The live tag is matched against a list
			// that holds it and judged against one whose highest tag is below it, since MatchVersion returns
			// no tag the list does not hold: the refusal that fires first is that the judged list holds no
			// such release tag, and the window's own rule - neither the highest nor the one immediately below
			// it - refuses it too.
			name:      "a version higher than the highest tag",
			text:      "v0.17.0",
			matchTags: []string{"v0.16.0", "v0.16.1", "v0.17.0"},
			judged:    []string{"v0.16.0", "v0.16.1"},
			bound:     windowBound,
			at:        time.Hour,
			wantErr:   true,
		},
		{
			name:      "a two-digit minor is higher than a one-digit one",
			text:      "v0.10.0",
			matchTags: []string{"v0.9.0", "v0.10.0"},
			bound:     windowBound,
			at:        1000 * time.Hour,
		},
		{
			name:      "the tag immediately below the two-digit minor, long after the window",
			text:      "v0.9.0",
			matchTags: []string{"v0.9.0", "v0.10.0"},
			bound:     windowBound,
			at:        1000 * time.Hour,
			wantErr:   true,
		},
		{
			name:      "the tag immediately below the two-digit minor, inside the window",
			text:      "v0.9.0",
			matchTags: []string{"v0.9.0", "v0.10.0"},
			bound:     windowBound,
			at:        time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			live := livecheck.Tag{}
			if !tc.zeroTag {
				live = windowTag(t, tc.matchTags, tc.text)
			}

			tags := tc.judged
			if tags == nil {
				tags = tc.matchTags
			}

			err := livecheck.AcceptLive(live, tags, livecheck.Window{
				Anchor: windowAnchor,
				Bound:  tc.bound,
				Now:    windowAnchor.Add(tc.at),
			})

			if tc.wantErr {
				if err == nil {
					t.Fatalf("AcceptLive(%q, %v, anchored %v, bound %v, now %v) accepted the live version, want an error: %s",
						live.String(), tags, windowAnchor.Format(time.RFC3339), tc.bound, windowAnchor.Add(tc.at).Format(time.RFC3339), tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("AcceptLive(%q, %v, anchored %v, bound %v, now %v) failed: %v: %s",
					live.String(), tags, windowAnchor.Format(time.RFC3339), tc.bound, windowAnchor.Add(tc.at).Format(time.RFC3339), err, tc.name)
			}
		})
	}
}
