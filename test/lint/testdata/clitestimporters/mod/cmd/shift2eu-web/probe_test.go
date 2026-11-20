// A test of the site's glue that runs the CLI is refused by both rules that
// hold it: web-root-tests-never-run-the-cli (PRD #297) and
// cli-only-in-named-tests (issue #345).
package main

import _ "github.com/timothymamo/shift2eu/internal/cli" // want "import 'github.com/timothymamo/shift2eu/internal/cli' is not allowed from list 'web-root-tests-never-run-the-cli'" "import 'github.com/timothymamo/shift2eu/internal/cli' is not allowed from list 'cli-only-in-named-tests'"
