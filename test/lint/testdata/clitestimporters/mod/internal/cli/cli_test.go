// The CLI's black-box tests import the package under test, which
// cli-only-in-named-tests allows (issue #345): no want comment.
package cli_test

import _ "github.com/timothymamo/shift2eu/internal/cli"
