// The CLI composition root's tests import no CLI today, so they are no named
// importer (issue #345).
package main

import _ "github.com/timothymamo/shift2eu/internal/cli" // want "import 'github.com/timothymamo/shift2eu/internal/cli' is not allowed from list 'cli-only-in-named-tests'"
