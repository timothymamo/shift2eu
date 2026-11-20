// A test of the web adapter that runs the CLI couples two driving adapters
// (issue #345), so cli-only-in-named-tests refuses it, and a package below
// internal/cli as well: the second import holds the rule's prefix deny entry,
// internal/cli/.
package web_test

import (
	_ "github.com/timothymamo/shift2eu/internal/cli"     // want "import 'github.com/timothymamo/shift2eu/internal/cli' is not allowed from list 'cli-only-in-named-tests'"
	_ "github.com/timothymamo/shift2eu/internal/cli/sub" // want "import 'github.com/timothymamo/shift2eu/internal/cli/sub' is not allowed from list 'cli-only-in-named-tests'"
)
