// A test of the HTTP API that runs the CLI couples two driving adapters
// (issue #345), so cli-only-in-named-tests refuses it.
package httpapi_test

import _ "github.com/timothymamo/shift2eu/internal/cli" // want "import 'github.com/timothymamo/shift2eu/internal/cli' is not allowed from list 'cli-only-in-named-tests'"
