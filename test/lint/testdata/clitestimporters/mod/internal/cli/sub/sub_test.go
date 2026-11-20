// A test of a package below internal/cli is no named importer (issue #345).
package sub_test

import _ "github.com/timothymamo/shift2eu/internal/cli" // want "import 'github.com/timothymamo/shift2eu/internal/cli' is not allowed from list 'cli-only-in-named-tests'"
