// Package foo stands for a package outside the composition roots that no
// strict rule holds: internal/config, or any other package of the module.
// adapters-are-built-in-cmd is the only rule that refuses an adapter import
// here, and it is lax, so this package shows that rule alone stops one: a
// package a strict rule holds would be refused by that rule too, which would
// keep the probe green if the lax rule stopped firing.
//
// It imports a package under each adapter subtree and the package at the root of
// each subtree. The subtree entries end in "/" and cover only what is under
// them, so the root packages are the part of each subtree that the rule must
// refuse with an entry of its own (issue #119).
package foo

import (
	"github.com/timothymamo/shift2eu/internal/adapter/report"               // want "import 'github.com/timothymamo/shift2eu/internal/adapter/report' is not allowed from list 'adapters-are-built-in-cmd': adapters are constructed only in cmd/shift2eu, cmd/shift2eu-web and ci/sitebuild"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"      // want "import 'github.com/timothymamo/shift2eu/internal/adapter/report/markdown' is not allowed from list 'adapters-are-built-in-cmd': adapters are constructed only in cmd/shift2eu, cmd/shift2eu-web and ci/sitebuild"
	"github.com/timothymamo/shift2eu/internal/adapter/source"               // want "import 'github.com/timothymamo/shift2eu/internal/adapter/source' is not allowed from list 'adapters-are-built-in-cmd': adapters are constructed only in cmd/shift2eu, cmd/shift2eu-web and ci/sitebuild"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile" // want "import 'github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile' is not allowed from list 'adapters-are-built-in-cmd': adapters are constructed only in cmd/shift2eu, cmd/shift2eu-web and ci/sitebuild"
	"github.com/timothymamo/shift2eu/internal/adapter/target"               // want "import 'github.com/timothymamo/shift2eu/internal/adapter/target' is not allowed from list 'adapters-are-built-in-cmd': adapters are constructed only in cmd/shift2eu, cmd/shift2eu-web and ci/sitebuild"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"      // want "import 'github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud' is not allowed from list 'adapters-are-built-in-cmd': adapters are constructed only in cmd/shift2eu, cmd/shift2eu-web and ci/sitebuild"
)

// Names names the adapters a package outside the composition roots must not
// reach.
const Names = report.Name + markdown.Name +
	source.Name + inventoryfile.Name + target.Name + ovhcloud.Name
