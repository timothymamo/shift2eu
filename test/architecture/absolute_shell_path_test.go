// The table test of the workflow path check's predicate (issue #655 point 4).
// The black-box test next door drives absoluteShellPath through the workflow
// document, which holds one invocation per case; here each value is its own
// case, so a value the check must refuse is pinned even while no workflow line
// carries it.

package architecture_test

import "testing"

// TestAbsoluteShellPath pins what the workflow's path check accepts: an
// absolute shell path, quoted or not, and the two variables' own directories.
// A path after /, ${RUNNER_TEMP}/ or ${GITHUB_WORKSPACE}/ that is not clean is
// refused, and so is a value whose quotes do not pair up, because the core's
// LoadResult and LoadPrevious refuse a path that is not absolute and clean
// (publish.ErrConfig, exit 9), and such a value is the one the workflow can
// pass that the program refuses.
func TestAbsoluteShellPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		// Accepted: absolute paths, clean, quoted or not.
		{name: "a path under the root", value: "/tmp/work/result.json", want: true},
		{name: "a double-quoted path under the root", value: `"/tmp/work/result.json"`, want: true},
		{name: "a single-quoted path under the root", value: `'/tmp/work/result.json'`, want: true},
		{name: "a path under the runner's temporary directory", value: "${RUNNER_TEMP}/bin/gh", want: true},
		{name: "a path under the workspace", value: "${GITHUB_WORKSPACE}/docs/security/audits", want: true},
		{name: "a quoted path under the workspace", value: `"${GITHUB_WORKSPACE}/docs/security/audits"`, want: true},
		{name: "the runner's temporary directory whole", value: "${RUNNER_TEMP}", want: true},
		{name: "the workspace whole", value: "${GITHUB_WORKSPACE}", want: true},

		// Refused: relative, and every path that is not clean.
		{name: "a relative path", value: "docs/security/audits", want: false},
		{name: "no value", value: "", want: false},
		{name: "a dot segment under the workspace", value: "${GITHUB_WORKSPACE}/docs/../reports", want: false},
		{name: "a quoted dot segment under the workspace", value: `"${GITHUB_WORKSPACE}/docs/../reports"`, want: false},
		{name: "an empty segment under the workspace", value: "${GITHUB_WORKSPACE}//docs/security/audits", want: false},
		{name: "a trailing slash under the workspace", value: "${GITHUB_WORKSPACE}/docs/security/audits/", want: false},
		{name: "an empty segment under the root", value: "/tmp//x", want: false},
		{name: "a dot segment under the root", value: "/a/../b", want: false},
		{name: "a quoted dot segment under the root", value: `"/a/../b"`, want: false},
		{name: "a current-directory segment under the root", value: "/a/./b", want: false},
		{name: "a trailing slash under the root", value: "/tmp/x/", want: false},
		{name: "a dot segment under the runner's temporary directory", value: "${RUNNER_TEMP}/../x", want: false},
		{name: "an empty segment under the runner's temporary directory", value: "${RUNNER_TEMP}//result.json", want: false},
		{name: "a trailing slash under the runner's temporary directory", value: "${RUNNER_TEMP}/dir/", want: false},

		// Refused: quotes that do not pair up.
		{name: "a leading quote without a trailing one", value: `"${GITHUB_WORKSPACE}/x`, want: false},
		{name: "a trailing quote without a leading one", value: `/tmp/x"`, want: false},
		{name: "mixed quotes", value: `"/tmp/x'`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := absoluteShellPath(tc.value); got != tc.want {
				t.Errorf("absoluteShellPath(%q) = %t, want %t: an absolute and clean path, or one of the two variables whole", tc.value, got, tc.want)
			}
		})
	}
}
