package architecture_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// pinnedDepguardRules is a copy of every depguard rule of .golangci.yml: the
// list-mode of each, its files list, its allow list and the pkg of each deny
// entry, rule by rule, in document order. Issue #80 is why it is here: a merge
// conflict resolution dropped the whole reporters-stay-offline rule and the
// exact jsondoc$ allow entry of adapters-are-built-in-cmd, no test noticed, and
// the dependency rule was quietly looser than the architecture it enforces.
//
// Change .golangci.yml and this copy in the same commit, on purpose, and let a
// reviewer read the edit as a change to the architecture rules: a rule, an
// entry or a list-mode that differs fails TestDepguardRulesArePinned, and
// TestDepguardPinCatchesMutations proves the pin would notice.
//
// The deny entries are pinned by their pkg only: a reworded desc is not a
// weakening of the rule. app-tests-use-fakes-not-adapters shares its deny list
// with app-uses-ports-not-adapters through a YAML anchor, so the entries listed
// under it are the ones the alias stands for.
var pinnedDepguardRules = []check.DepguardRule{
	{
		Name:     "domain-is-pure",
		ListMode: "strict",
		Files: []string{
			"**/internal/domain/**",
			"!$test",
		},
		Allow: []string{
			"errors$",
			"fmt$",
			"math/big$",
			"net/netip$",
			"net/url$",
			"slices$",
			"strconv$",
			"strings$",
			"time$",
			"unicode$",
			"unicode/utf8$",
			"github.com/timothymamo/shift2eu/internal/domain/assessment$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/estimate$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/mapping$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: []string{
			"os",
			"net",
			"syscall",
			"unsafe",
			"plugin",
			"runtime/cgo",
		},
	},
	{
		Name:     "app-uses-ports-not-adapters",
		ListMode: "strict",
		Files: []string{
			"**/internal/app/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"errors$",
			"fmt$",
			"io$",
			"strings$",
			"github.com/timothymamo/shift2eu/internal/domain/assessment$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/estimate$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/mapping$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter",
			"github.com/timothymamo/shift2eu/internal/cli",
			"github.com/aws/aws-sdk-go-v2",
			"github.com/Azure/azure-sdk-for-go",
			"cloud.google.com/go",
			"google.golang.org/api",
			"net/http",
			"net/rpc",
			"net/smtp",
			"net/mail",
			"net/textproto",
			"crypto/tls",
			"net$",
			"os/exec",
			"os/user",
			"os/signal",
			"os$",
			"syscall",
			"plugin",
			"io/ioutil",
			"log/syslog",
			"text/template",
			"html/template",
			"archive/zip",
			"debug/",
			"go/build$",
			"go/importer",
			"runtime/debug",
			"go/parser",
			"mime/multipart",
			"runtime/coverage",
			"unsafe",
		},
	},
	{
		// The application layer's _test.go files (issue #144): the rule is a
		// strict allow list, as the production layers' rules are, so it names
		// what an application-layer test may import and refuses everything
		// else. The standard packages are exactly the ones the application
		// layer's tests import today (issue #144's review, finding B1): bytes,
		// context, which the use case takes, errors, io, slices, strings, sync
		// for a fake's mutex, and testing. Every one of them is pure:
		// errors, slices and strings are on the domain's pure list
		// (domain-is-pure above), and the five beyond it (bytes, context, io,
		// sync, testing) are the extras
		// TestAppTestsRuleAllowsOnlyPurePackagesAndItsExtras holds, each with
		// the reason a test gives; that test fails when an extra is no entry
		// of this allow list or is one the domain's list already names, so the
		// extras cannot go stale. The domain's fmt, time, net/netip, net/url,
		// strconv, unicode and unicode/utf8 are deliberately not here: the
		// application layer's tests import none of them, and fmt and time can
		// print to stdout, read stdin and read zoneinfo files and the clock,
		// which no forbidigo group refuses in a test. A test that needs one
		// adds it to this list, that test and .golangci.yml in the same commit.
		// The module's packages are the application package under test, the
		// six domain packages and internal/testutil; go-cmp is the one
		// third-party package a test may import. Every entry is exact, and no
		// deny entry names a package an allow entry allows.
		//
		// The deny list is the one app-uses-ports-not-adapters writes, which the
		// alias *app-deny shares: under a strict list it only says why a
		// package the allow list already refuses is refused.
		//
		// The files: list holds the _test.go files of internal/app and of the
		// layer's subdirectories, and no production file, so app-uses-ports-
		// not-adapters holds those. RuleDirs reads the pair as the test files of
		// the directory alone (RuleFiles.TestsOnly, issue #144), so the package
		// check holds the test imports of every package under internal/app to
		// this list, package by package for every release target, and
		// TestAppTestsUseFakesNotAdaptersProbe (test/lint) holds the imports a
		// test must still be refused.
		Name:     "app-tests-use-fakes-not-adapters",
		ListMode: "strict",
		Files: []string{
			"**/internal/app/**/*_test.go",
			"**/internal/app/*_test.go",
		},
		Allow: []string{
			"bytes$",
			"context$",
			"errors$",
			"github.com/google/go-cmp/cmp$",
			"github.com/timothymamo/shift2eu/internal/app/analysis$",
			"github.com/timothymamo/shift2eu/internal/domain/assessment$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/estimate$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/mapping$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
			"github.com/timothymamo/shift2eu/internal/testutil$",
			"io$",
			"slices$",
			"strings$",
			"sync$",
			"testing$",
		},
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter",
			"github.com/timothymamo/shift2eu/internal/cli",
			"github.com/aws/aws-sdk-go-v2",
			"github.com/Azure/azure-sdk-for-go",
			"cloud.google.com/go",
			"google.golang.org/api",
			"net/http",
			"net/rpc",
			"net/smtp",
			"net/mail",
			"net/textproto",
			"crypto/tls",
			"net$",
			"os/exec",
			"os/user",
			"os/signal",
			"os$",
			"syscall",
			"plugin",
			"io/ioutil",
			"log/syslog",
			"text/template",
			"html/template",
			"archive/zip",
			"debug/",
			"go/build$",
			"go/importer",
			"runtime/debug",
			"go/parser",
			"mime/multipart",
			"runtime/coverage",
			"unsafe",
		},
	},
	{
		// internal/testutil (issue #144, the auditor's finding M-1 and the
		// reviewer's S1): the application layer's tests may import it, and it
		// reads and writes golden files, so it imports os and path/filepath
		// today. Nothing held those imports to anything: a helper that reached
		// the network (net/http), started a program (os/exec) or read the
		// environment (os.Getenv) would have given an application test I/O with
		// lint green, since the test's own rule allows internal/testutil
		// whatever it imports. So testutil has a strict rule of its own, and
		// its allow list is exactly the imports its files make today, each
		// exact: the standard packages, the two domain packages it builds
		// values of, and internal/testutil itself, which its external test
		// package imports. A new import needs an entry here, in .golangci.yml
		// and in this pin, in the same commit, and a reviewer reads it as a
		// widening of the helpers every test shares.
		//
		// The files: list names the directory whole and no !$test, so the rule
		// holds the helpers' _test.go files too: a test of testutil runs with
		// the same privileges as the helpers. RuleDirs reads it as one
		// directory holding test files (RuleFiles.Tests), so
		// TestReleaseTargetDependencies holds the package's own and its test
		// files' imports to this list, package by package for every release
		// target. An import rule sees no call, so the forbidigo group
		// testutil-no-env refuses the calls that reach the environment and
		// start a process, which os and path/filepath alone cannot say.
		Name:     "testutil-imports",
		ListMode: "strict",
		Files: []string{
			"**/internal/testutil/**",
		},
		Allow: []string{
			"bytes$",
			"encoding/json$",
			"flag$",
			"fmt$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/testutil$",
			"maps$",
			"math$",
			"os$",
			"path/filepath$",
			"reflect$",
			"regexp$",
			"slices$",
			"strconv$",
			"strings$",
			"testing$",
			"time$",
		},
		Deny: nil,
	},
	{
		Name:     "cli-gets-adapters-from-cmd",
		ListMode: "strict",
		Files: []string{
			"**/internal/cli/**",
			"!$test",
		},
		Allow: []string{
			"bytes$",
			"context$",
			"errors$",
			"flag$",
			"fmt$",
			"io$",
			"io/fs$",
			"maps$",
			"os$",
			"path/filepath$",
			"slices$",
			"strconv$",
			"strings$",
			"unicode$",
			"unicode/utf8$",
			"github.com/timothymamo/shift2eu/internal/app/analysis$",
			"github.com/timothymamo/shift2eu/internal/domain/assessment$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/estimate$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/mapping$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
			"github.com/timothymamo/shift2eu/internal/version$",
		},
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter",
		},
	},
	{
		// The build metadata (issue #152, the security audit's M-3). Both
		// driving adapters print internal/version, and no rule held it: a probe
		// that imported os and os/exec there passed lint. A composition root that
		// may not read the environment or start a process could hand either to a
		// version package nobody had thought to deny, and the call happened one
		// import deeper. The rule is a strict allow list of exactly what the
		// package imports today, each entry exact: fmt, which String formats the
		// build line with. The three variables the build sets through -X are not
		// imports, so the list never needs one for them.
		//
		// The files: entry takes the _test.go file out (!$test), as the finding
		// states it: the build line's test imports strings and testing, which a
		// strict allow list of the package's own imports would refuse, and a
		// second list for one test file buys nothing. RuleDirs reads the
		// directory with no test files, so TestReleaseTargetDependencies holds
		// the package's production imports to this list, package by package for
		// every release target, and the CLI's release listing must hold the
		// package (internal/cli imports it), so the check cannot pass over it.
		Name:     "version-is-build-metadata",
		ListMode: "strict",
		Files: []string{
			"**/internal/version/**",
			"!$test",
		},
		Allow: []string{
			"fmt$",
		},
		Deny: nil,
	},
	{
		// The composition root (issue #101): main wires every adapter into the
		// application, so it may import every package of the module, named by
		// the one prefix entry, and the standard packages it imports today,
		// each exact. A third-party import is refused by the allow list, and
		// the cloud SDKs by the deny list too, as in app-uses-ports-not-adapters.
		// A sibling such as cmd/shift2eu-server gets a rule of its own, written
		// the same way, with its own files: entry.
		Name:     "composition-root",
		ListMode: "strict",
		Files: []string{
			"**/cmd/shift2eu/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"os$",
			"os/signal$",
			"syscall$",
			"github.com/timothymamo/shift2eu/",
		},
		Deny: []string{
			"github.com/aws/aws-sdk-go-v2",
			"github.com/Azure/azure-sdk-for-go",
			"cloud.google.com/go",
			"google.golang.org/api",
		},
	},
	{
		// The server's composition root (PRD #103, milestone 3), the sibling of
		// composition-root and written the same way: its files: list names
		// cmd/shift2eu-server and nothing beside it, its allow list is the module
		// path as the one prefix entry plus the standard packages the server
		// imports today, each exact, and its deny list names the four cloud
		// SDKs. A third-party package the allow list does not name is refused by
		// the allow list, so the server cannot gain a dependency unseen.
		//
		// test/architecture reads the rule the way it reads composition-root:
		// TestCompositionRootRulesAreChecked pins its files, allow and prefix
		// entries and its skip of the shared layer listing,
		// TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget holds
		// cmd/shift2eu-server to it on every release target, and
		// TestCompositionRootRulesHoldTheirProbesForEveryReleaseTarget fails a
		// fixture server root that imports a third-party library or a cloud SDK.
		Name:     "server-composition-root",
		ListMode: "strict",
		Files: []string{
			"**/cmd/shift2eu-server/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"errors$",
			"flag$",
			"fmt$",
			"io$",
			"log$",
			"math$",
			"net$",
			"net/http$",
			"os$",
			"os/signal$",
			"runtime$",
			"runtime/debug$",
			"slices$",
			"strconv$",
			"strings$",
			"sync$",
			"syscall$",
			"time$",
			"github.com/timothymamo/shift2eu/",
		},
		Deny: []string{
			"github.com/aws/aws-sdk-go-v2",
			"github.com/Azure/azure-sdk-for-go",
			"cloud.google.com/go",
			"google.golang.org/api",
		},
	},
	{
		Name:     "adapters-are-built-in-cmd",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!$test",
			"!**/cmd/shift2eu/**",
			"!**/cmd/shift2eu-server/**",
		},
		Allow: []string{
			"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc$",
			"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot$",
		},
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter/internal/",
			"github.com/timothymamo/shift2eu/internal/adapter/report/",
			"github.com/timothymamo/shift2eu/internal/adapter/report$",
			"github.com/timothymamo/shift2eu/internal/adapter/request/",
			"github.com/timothymamo/shift2eu/internal/adapter/request$",
			"github.com/timothymamo/shift2eu/internal/adapter/source/",
			"github.com/timothymamo/shift2eu/internal/adapter/source$",
			"github.com/timothymamo/shift2eu/internal/adapter/target/",
			"github.com/timothymamo/shift2eu/internal/adapter/target$",
		},
	},
	{
		Name:     "cli-and-cmd-imported-only-by-cmd",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!$test",
			"!**/cmd/shift2eu/**",
			"!**/internal/cli/**",
		},
		Allow: nil,
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/cli$",
			"github.com/timothymamo/shift2eu/internal/cli/",
			"github.com/timothymamo/shift2eu/cmd$",
			"github.com/timothymamo/shift2eu/cmd/",
		},
	},
	{
		Name:     "httpapi-gets-adapters-from-cmd",
		ListMode: "strict",
		Files: []string{
			"**/internal/httpapi/**",
			"!$test",
		},
		Allow: []string{
			"bytes$",
			"context$",
			"crypto/sha256$",
			"crypto/subtle$",
			"encoding/json$",
			"errors$",
			"fmt$",
			"io$",
			"maps$",
			"mime$",
			"net/http$",
			"net/url$",
			"runtime$",
			"slices$",
			"sort$",
			"strconv$",
			"strings$",
			"sync$",
			"time$",
			"unicode/utf8$",
			"github.com/timothymamo/shift2eu/internal/app/analysis$",
			"github.com/timothymamo/shift2eu/internal/domain/assessment$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/estimate$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/mapping$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
			"github.com/timothymamo/shift2eu/internal/version$",
		},
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter",
			"github.com/timothymamo/shift2eu/internal/cli",
			"github.com/timothymamo/shift2eu/cmd",
			"os",
			"syscall",
			"plugin",
			"unsafe",
		},
	},
	{
		Name:     "httpapi-imported-only-by-the-server",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!**/test/apiparity/**",
			"!**/cmd/shift2eu-server/**",
			"!**/internal/httpapi/**",
		},
		Allow: nil,
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/httpapi$",
			"github.com/timothymamo/shift2eu/internal/httpapi/",
		},
	},
	{
		Name:     "snapshot-reader-only-in-targets",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!$test",
			"!**/internal/adapter/target/**",
		},
		Allow: nil,
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot",
		},
	},
	{
		Name:     "target-snapshot-reader-parses-bytes",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/target/snapshot/**",
			"!$test",
		},
		Allow: []string{
			"errors$",
			"fmt$",
			"strconv$",
			"strings$",
			"time$",
			"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: nil,
	},
	{
		Name:     "target-adapters-read-snapshots-through-the-reader",
		ListMode: "lax",
		Files: []string{
			"**/internal/adapter/target/**",
			"!$test",
		},
		Allow: nil,
		Deny: []string{
			"encoding/json",
		},
	},
	{
		Name:     "adapter-helpers-do-no-io",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/internal/**",
			"!$test",
		},
		Allow: []string{
			"bytes$",
			"encoding/json$",
			"errors$",
			"fmt$",
			"io$",
			"slices$",
			"strconv$",
			"strings$",
			"unicode$",
			"unicode/utf16$",
			"unicode/utf8$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: []string{
			"io/fs",
			"io/ioutil",
			"net",
			"os",
			"plugin",
			"runtime/cgo",
			"syscall",
			"unsafe",
		},
	},
	{
		Name:     "jsondoc-parses-bytes",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/internal/jsondoc/**",
			"!$test",
		},
		Allow: []string{
			"bytes$",
			"encoding/json$",
			"errors$",
			"fmt$",
			"io$",
			"slices$",
			"strconv$",
			"strings$",
			"unicode$",
			"unicode/utf16$",
			"unicode/utf8$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: nil,
	},
	{
		Name:     "inventory-file-reader-reads-one-file",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/source/inventoryfile/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"errors$",
			"fmt$",
			"io$",
			"io/fs$",
			"os$",
			"strconv$",
			"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: []string{
			"encoding/json",
		},
	},
	{
		Name:     "request-decoders-parse-bytes",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/request/**",
			"!$test",
		},
		Allow: []string{
			"errors$",
			"fmt$",
			"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc$",
		},
		Deny: []string{
			"encoding/json",
			"net",
			"os",
			"github.com/timothymamo/shift2eu/internal/httpapi",
		},
	},
	{
		Name:     "cloud-adapters-reach-the-cloud-through-the-sdk",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/source/**",
			"!**/internal/adapter/source/inventoryfile/**",
			"!**/internal/adapter/source/sourcetest/**",
		},
		Allow: []string{
			"bytes$",
			"context$",
			"errors$",
			"fmt$",
			"iter$",
			"log/slog$",
			"maps$",
			"net/netip$",
			"net/url$",
			"slices$",
			"sort$",
			"strconv$",
			"strings$",
			"sync$",
			"time$",
			"unicode$",
			"unicode/utf8$",
			"testing$",
			"github.com/aws/aws-sdk-go-v2/",
			"github.com/aws/smithy-go$",
			"github.com/Azure/azure-sdk-for-go/sdk/",
			"cloud.google.com/go/",
			"google.golang.org/api/googleapi$",
			"google.golang.org/api/iterator$",
			"google.golang.org/api/option$",
			"google.golang.org/grpc/codes$",
			"google.golang.org/grpc/status$",
			"github.com/google/go-cmp/cmp$",
			"github.com/google/go-cmp/cmp/",
			"github.com/timothymamo/shift2eu/internal/adapter/source/aws$",
			"github.com/timothymamo/shift2eu/internal/adapter/source/azure$",
			"github.com/timothymamo/shift2eu/internal/adapter/source/gcp$",
			"github.com/timothymamo/shift2eu/internal/adapter/source/sourcetest$",
			"github.com/timothymamo/shift2eu/internal/app/analysis$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
			"github.com/timothymamo/shift2eu/internal/testutil$",
		},
		Deny: []string{
			"reflect",
			"text/template",
			"html/template",
			"unsafe",
			"os",
			"plugin",
			"syscall",
			"golang.org/x/sys",
			"runtime/cgo",
			"net",
			"database/sql",
		},
	},
	{
		Name:     "cloud-sdks-only-in-provider-adapters",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!**/internal/adapter/source/aws/**",
			"!**/internal/adapter/source/azure/**",
			"!**/internal/adapter/source/gcp/**",
			"!**/internal/adapter/target/ovhcloud/**",
			"!**/internal/adapter/target/scaleway/**",
			"!**/internal/adapter/target/upcloud/**",
		},
		Allow: nil,
		Deny: []string{
			"github.com/aws/",
			"github.com/Azure/",
			"cloud.google.com/go",
			"google.golang.org/api",
			"google.golang.org/grpc",
			"github.com/googleapis/",
			"golang.org/x/oauth2",
			"github.com/ovh/",
			"github.com/scaleway/",
			"github.com/UpCloudLtd/",
		},
	},
	{
		Name:     "embedded-snapshot-targets-stay-offline",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/target/ovhcloud/**",
			"**/internal/adapter/target/scaleway/**",
			"**/internal/adapter/target/upcloud/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"embed$",
			"fmt$",
			"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
		Deny: []string{
			"io/fs",
			"io/ioutil",
			"net",
			"os",
			"plugin",
			"syscall",
			"unsafe",
		},
	},
	{
		Name:     "reporters-stay-offline",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/report/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"fmt$",
			"io$",
			"strconv$",
			"strings$",
			"encoding/json$",
			"github.com/timothymamo/shift2eu/internal/app/analysis$",
			"github.com/timothymamo/shift2eu/internal/domain/assessment$",
			"github.com/timothymamo/shift2eu/internal/domain/estimate$",
			"github.com/timothymamo/shift2eu/internal/domain/mapping$",
		},
		Deny: []string{
			"io/fs",
			"io/ioutil",
			"net",
			"os/exec",
			"os/signal",
			"os/user",
			"os$",
			"plugin",
			"runtime/cgo",
			"syscall",
			"unsafe",
		},
	},
	{
		Name:     "testutil-only-in-tests",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!$test",
			"!**/internal/testutil/**",
		},
		Allow: nil,
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/testutil",
		},
	},
	{
		Name:     "contract-suites-only-in-tests",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!$test",
			"!**/internal/adapter/source/sourcetest/**",
			"!**/internal/adapter/target/targettest/**",
		},
		Allow: nil,
		Deny: []string{
			"github.com/timothymamo/shift2eu/internal/adapter/source/sourcetest",
			"github.com/timothymamo/shift2eu/internal/adapter/target/targettest",
		},
	},
	{
		Name:     "go-cmp-only-in-tests",
		ListMode: "lax",
		Files: []string{
			"$all",
			"!$test",
		},
		Allow: nil,
		Deny: []string{
			"github.com/google/go-cmp",
		},
	},
	{
		Name:     "e2e-is-black-box",
		ListMode: "strict",
		Files: []string{
			"**/test/e2e/**",
		},
		Allow: []string{
			"$gostd",
			"github.com/google/go-cmp",
			"github.com/timothymamo/shift2eu/internal/testutil$",
		},
		Deny: []string{
			"github.com/timothymamo/shift2eu/",
			"github.com/aws/aws-sdk-go-v2",
			"github.com/Azure/azure-sdk-for-go",
			"cloud.google.com/go",
			"google.golang.org/api",
		},
	},
	{
		// The composition roots' _test.go files (issue #111). The two strict
		// rules above skip test files (!$test), and a test is compiled into
		// the same package and runs with the same process: it could start a
		// program, load a plugin, hand a path to a CGI handler, or reach a
		// foreign symbol through unsafe and //go:linkname. The rule is lax,
		// so its deny list alone decides and no allow list has to follow
		// every pure import a test makes; it is no layer the package check
		// reads, which is why it is no strict rule
		// (TestEveryStrictRuleIsChecked). syscall/js is pinned here and by no
		// probe line, as it builds for js/wasm only
		// (TestCompositionRootTestsReachNoForeignCodeProbe). os/user is denied
		// since issue #152: a test that looks a user up in the OS database reads
		// the home directory os.Getenv and os.UserHomeDir are refused for, one
		// package further out.
		Name:     "composition-root-tests",
		ListMode: "lax",
		Files: []string{
			"**/cmd/shift2eu/*_test.go",
			"**/cmd/shift2eu/**/*_test.go",
			"**/cmd/shift2eu-server/*_test.go",
			"**/cmd/shift2eu-server/**/*_test.go",
			"**/internal/cli/*_test.go",
			"**/internal/cli/**/*_test.go",
			"**/internal/httpapi/*_test.go",
			"**/internal/httpapi/**/*_test.go",
		},
		Allow: nil,
		Deny: []string{
			"go/build",
			"net/http/cgi",
			"os/exec",
			"os/user",
			"plugin",
			"runtime/cgo",
			"syscall/js",
			"unsafe",
		},
	},
	{
		// The runtime configuration a composition root reads (issue #152,
		// the auditor's finding M-A): internal/config loads it from flags,
		// the environment and an optional file, so it announces the environment
		// reads that need a forbidigo group of its own and an ADR before it may
		// import os. It has no code yet, so it allows nothing: allow: [] and
		// the one deny entry, os, as the config-is-pure comment in
		// .golangci.yml explains (issue #166). The day it imports a package,
		// the rule names it here in the same commit; an os import also drops
		// the os deny entry, as check.AllowList refuses a rule whose allow
		// list allows a package its deny list names.
		Name:     "config-is-pure",
		ListMode: "strict",
		Files: []string{
			"**/internal/config/**",
			"!$test",
		},
		Allow: []string{},
		Deny:  []string{"os"},
	},
	{
		// The target subtree's own documentation package (issue #152, the
		// auditor's finding M-A): internal/adapter/target holds doc.go and no
		// code, and its providers have strict rules of their own, which the
		// negated globs take out here. It imports nothing, so it allows
		// nothing: allow: [] and the one deny entry, os, as config-is-pure.
		// A provider added under it is named
		// here or in embedded-snapshot-targets-stay-offline in the same commit.
		Name:     "target-docs-are-pure",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/target/**",
			"!**/internal/adapter/target/ovhcloud/**",
			"!**/internal/adapter/target/scaleway/**",
			"!**/internal/adapter/target/upcloud/**",
			"!**/internal/adapter/target/snapshot/**",
			"!**/internal/adapter/target/targettest/**",
			"!$test",
		},
		Allow: []string{},
		Deny:  []string{"os"},
	},
	{
		// The contract suites every adapter runs (issue #152, the auditor's
		// finding L-A): they are production packages, and any _test.go file
		// may import them, so they may import only what they import today.
		// sourcetest and targettest are taken out of
		// cloud-adapters-reach-the-cloud-through-the-sdk by name; this is the
		// rule that holds them. time$ is targettest's, and the one entry the
		// shared rule lets sourcetest widen by.
		Name:     "contract-suites-are-pure",
		ListMode: "strict",
		Files: []string{
			"**/internal/adapter/source/sourcetest/**",
			"**/internal/adapter/target/targettest/**",
			"!$test",
		},
		Allow: []string{
			"context$",
			"errors$",
			"fmt$",
			"slices$",
			"testing$",
			"time$",
			"github.com/timothymamo/shift2eu/internal/app/analysis$",
			"github.com/timothymamo/shift2eu/internal/domain/catalog$",
			"github.com/timothymamo/shift2eu/internal/domain/inventory$",
			"github.com/timothymamo/shift2eu/internal/domain/taxonomy$",
			"github.com/timothymamo/shift2eu/internal/domain/text$",
		},
	},
	{
		// The test trees' own documentation packages (issue #152, the auditor's finding
		// H-1): test/apiparity, test/architecture, test/container, test/devenv,
		// test/docs, test/lint, test/openapibundle (issue #139) and test/examples
		// (issue #179) hold doc.go and no code, and each is a production package a
		// composition root can import, because the roots' allow lists name the module
		// path as a prefix entry. Go's internal
		// rule keeps test/internal/... and test/architecture/internal/... out of cmd/
		// and internal/ and nothing keeps these out, so they get a rule of their own. It
		// is one shared strict rule that allows nothing: allow: [] and the one deny entry,
		// os, as config-is-pure. The day one of these packages imports a package, it
		// names it here and in .golangci.yml in the same commit.
		// test/e2e belongs here too since issue #152 (the reviewer's B1, the auditor's
		// L-1): its doc.go carries no build tag, so test/e2e is a production package a
		// composition root may import, and e2e-is-black-box, the only rule that held it,
		// allows $gostd. An untagged file there that read os.Getenv and started
		// exec.Command stayed lint green with every test passing. !$test takes the e2e
		// test files out (depguard applies negated globs whatever their position), so
		// they stay on e2e-is-black-box alone, which is the black-box rule with a $gostd
		// of its own. That same negation takes out the other test files, whose imports
		// these packages' tests would not fit this list: a test of test/architecture
		// reads the repository, go-cmp included. The nested internal tree under
		// test/architecture is taken out with a negated glob, as a rule's files: list
		// cannot say "this directory only": without it the rule would hold
		// test/architecture/internal/check, which reads os and os/exec, and refuse them.
		Name:     testDocsRule,
		ListMode: "strict",
		Files: []string{
			"**/test/apiparity/**",
			"**/test/architecture/**",
			"!**/test/architecture/internal/**",
			"**/test/container/**",
			"**/test/devenv/**",
			"**/test/docs/**",
			"**/test/e2e/**",
			"**/test/examples/**",
			"**/test/lint/**",
			"**/test/openapibundle/**",
			"!$test",
		},
		Allow: []string{},
		Deny:  []string{"os"},
	},
}

// TestDepguardRulesArePinned pins every depguard rule of .golangci.yml against
// pinnedDepguardRules. A rule removed, weakened or added without this copy
// following it fails with the rule named, so no merge can drop a piece of the
// dependency rule unseen (issue #80).
func TestDepguardRulesArePinned(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	rules, err := check.DepguardRules(golangci)
	if err != nil {
		t.Fatalf("DepguardRules(.golangci.yml): %v", err)
	}

	if len(rules) != len(pinnedDepguardRules) {
		t.Errorf(".golangci.yml holds %d depguard rules, the pin holds %d", len(rules), len(pinnedDepguardRules))
	}

	for _, message := range check.PinnedRules(rules, pinnedDepguardRules) {
		t.Errorf("%s\n"+
			"the depguard rules are pinned: if .golangci.yml changed on purpose, update pinnedDepguardRules in this file in the same commit, and a reviewer reads that edit as a change to the architecture rules", message)
	}
}

// cliImportRule is the name of the rule that holds internal/cli's production
// imports (issue #152).
const cliImportRule = "cli-gets-adapters-from-cmd"

// cliImportRuleDenied are the packages the CLI's import rule must never allow,
// now that it is a strict allow list: a process start (os/exec, syscall and
// plugin, whose code runs in the CLI's process), a raw system call, a pointer
// taken apart (unsafe), a signal handler that is not os/signal's, and the
// packages a root's test may not import either.
var cliImportRuleDenied = []string{
	"os/exec",
	"syscall",
	"unsafe",
	"plugin",
	"runtime/cgo",
	"os/signal",
	"net/http/cgi",
	"go/build",
}

// TestCLIImportRuleAllowsNoProcessOrPointer pins the shape issue #152 gave
// cli-gets-adapters-from-cmd: a strict allow list that names what internal/cli
// may import, instead of $gostd with a deny list beside it. $gostd allowed every
// standard package nobody had thought to deny, unsafe among them: a composition
// root that may not start a process or read the environment itself could hand
// the call to the CLI, and the deny list kept out only the names it listed. The
// allow list names os, which the CLI reads the inventory a user names with and
// creates the report the user asked for with, and neither syscall, unsafe nor
// os/exec, which it names for no purpose.
//
// The exact entries are pinned by TestDepguardRulesArePinned, whose copy the
// commit that edits .golangci.yml updates; this one holds the property the
// issue is about, so a rule rewritten entry by entry still has to keep it.
func TestCLIImportRuleAllowsNoProcessOrPointer(t *testing.T) {
	t.Parallel()

	rules, err := check.DepguardRules(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf("DepguardRules(.golangci.yml): %v", err)
	}

	var rule check.DepguardRule
	found := false
	for _, r := range rules {
		if r.Name == cliImportRule {
			rule, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf(".golangci.yml holds no depguard rule %s, so nothing holds internal/cli's imports", cliImportRule)
	}

	if rule.ListMode != "strict" {
		t.Errorf("the depguard rule %s has list-mode %q; a strict allow list says what the CLI may import, so a package added to the standard library later is refused until the list names it", cliImportRule, rule.ListMode)
	}
	if slices.Contains(rule.Allow, "$gostd") {
		t.Errorf("the depguard rule %s allows $gostd, which allows every standard package, syscall and unsafe among them; name the packages the CLI imports today, each ending in $", cliImportRule)
	}

	// allows reports whether an allow entry admits pkg, as depguard reads the
	// entries: one ending in $ is exact, and any other is a prefix. $gostd is
	// read above, so an entry here is a package path.
	allows := func(pkg string) bool {
		for _, entry := range rule.Allow {
			if exact, ok := strings.CutSuffix(entry, "$"); ok {
				if exact == pkg {
					return true
				}
				continue
			}
			if pkg == entry || strings.HasPrefix(pkg, entry) {
				return true
			}
		}
		return false
	}

	if !allows("os") {
		t.Errorf("the depguard rule %s does not allow os, which the CLI reads the inventory a user names (--inventory) and creates the report the user asked for (--output) with", cliImportRule)
	}
	for _, pkg := range cliImportRuleDenied {
		if allows(pkg) {
			t.Errorf("the depguard rule %s allows %s, which the CLI may not import: a root that may not start a process, read a directory from the environment or take a pointer apart could hand it the call", cliImportRule, pkg)
		}
	}
}

// versionRule is the name of the rule that holds internal/version's production
// imports (issue #152, the security audit's M-3).
const versionRule = "version-is-build-metadata"

// versionRuleDenied are the packages the build metadata's rule must never
// allow: everything a composition root or a driving adapter may not reach, which
// a version package nobody held could have handed it one import deeper. The
// allow list refuses them already, as a strict list refuses everything it does
// not name; this names the ones a careless widening (an os$ entry that lost its
// $, or a $gostd) would let back in.
var versionRuleDenied = []string{
	"os",
	"os/exec",
	"os/user",
	"os/signal",
	"syscall",
	"unsafe",
	"plugin",
	"net/http",
	"runtime/debug",
	"io/ioutil",
	"go/build",
}

// TestVersionRuleIsBuildMetadata pins the shape issue #152 gave
// version-is-build-metadata. internal/version is the build metadata internal/cli
// prints, and both driving adapters' import rules allow it, yet no rule held what
// it imports: a probe that imported os and os/exec there passed lint. A package
// allowed by a strict rule is a delegation path, because the rule says nothing of
// what that package does, so the build metadata gets a strict allow list of its
// own: exactly the packages it imports today, each ending in $, which is fmt
// alone (String formats the build line), and no $gostd.
//
// The exact entries are pinned by TestDepguardRulesArePinned, whose copy the
// commit that edits .golangci.yml updates; this one holds the property the
// finding is about, so a rule rewritten entry by entry still has to keep it.
func TestVersionRuleIsBuildMetadata(t *testing.T) {
	t.Parallel()

	rules, err := check.DepguardRules(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf("DepguardRules(.golangci.yml): %v", err)
	}

	var rule check.DepguardRule
	found := false
	for _, r := range rules {
		if r.Name == versionRule {
			rule, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf(".golangci.yml holds no depguard rule %s, so nothing holds internal/version's imports: both driving adapters may import it, and a composition root that may not read the environment or start a process could hand either to it", versionRule)
	}

	if rule.ListMode != "strict" {
		t.Errorf("the depguard rule %s has list-mode %q; a strict allow list says what the build metadata may import, so a package added to the standard library later is refused until the list names it", versionRule, rule.ListMode)
	}
	if slices.Contains(rule.Allow, "$gostd") {
		t.Errorf("the depguard rule %s allows $gostd, which allows every standard package, os, syscall and unsafe among them; name the packages internal/version imports today, each ending in $", versionRule)
	}

	// allows reports whether an allow entry admits pkg, as depguard reads the
	// entries: one ending in $ is exact, and any other is a prefix.
	allows := func(pkg string) bool {
		for _, entry := range rule.Allow {
			if exact, ok := strings.CutSuffix(entry, "$"); ok {
				if exact == pkg {
					return true
				}
				continue
			}
			if pkg == entry || strings.HasPrefix(pkg, entry) {
				return true
			}
		}
		return false
	}

	for _, pkg := range versionRuleDenied {
		if allows(pkg) {
			t.Errorf("the depguard rule %s allows %s, which the build metadata may not import: a composition root that may not read the environment, start a process or take a pointer apart could hand it the call one import deeper", versionRule, pkg)
		}
	}

	if got := entriesWithoutDollar(rule.Allow); !slices.Equal(got, []string{"fmt"}) {
		t.Errorf("the depguard rule %s allows %v, want [fmt]: the build metadata imports fmt and nothing else, so a new package here is a widening a reviewer reads in the same commit", versionRule, got)
	}
}

// entriesWithoutDollar returns the allow entries without their trailing $, which
// fail here when one is missing: an entry without $ is a prefix, which allows a
// whole tree of packages.
func entriesWithoutDollar(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if exact, ok := strings.CutSuffix(entry, "$"); ok {
			out = append(out, exact)
		}
	}
	slices.Sort(out)
	return out
}

// TestDepguardPinCatchesMutations proves the pin of TestDepguardRulesArePinned
// is not vacuous: it takes the real .golangci.yml text in memory and, for every
// rule, deletes the rule, deletes each entry of its files, allow and deny lists,
// drops the $ of each exact allow entry, adds an allow entry, adds a files
// exclusion and rewrites, deletes or adds the list-mode. A deletion and an
// addition weaken a rule alike, and the entries added here are the ones a
// careless edit adds. Every mutated document must make PinnedRules name the
// rule it is about, except the one mutation that breaks the document itself,
// which the reader must refuse rather than read as a weaker rule. A mutation
// that leaves the document unchanged fails, so no case here passes on nothing.
func TestDepguardPinCatchesMutations(t *testing.T) {
	t.Parallel()

	if len(pinnedDepguardRules) == 0 {
		t.Fatal("pinnedDepguardRules is empty, so this test would mutate nothing")
	}

	mutations := depguardMutations(t, string(readRepositoryFile(t, ".golangci.yml")))

	t.Logf("%d mutations of .golangci.yml", len(mutations))

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()

			requireMutationReported(t, mutation)
		})
	}
}

// depguardMutation is one weakening of the depguard rules: the name of its case,
// the rule it is about, the mutated lines of .golangci.yml and whether the
// reader is expected to refuse the mutation rather than read it as a different
// rule.
type depguardMutation struct {
	name  string
	rule  string
	lines []string
	// wantParseError is set on the one mutation that breaks the document
	// itself: deleting the rule that holds the deny list its alias needs
	// leaves the alias with no anchor, and the reader refuses the document.
	wantParseError bool
}

// depguardMutations builds every weakening of .golangci.yml the pin must catch,
// before any case runs: each mutation must change the document, and at least one
// must exist, so no case can pass on nothing. It fails when the entry walk does
// not find as many entries as the pin holds, so the mutations below cannot
// quietly cover a list's first entry only.
func depguardMutations(t *testing.T, document string) []depguardMutation {
	t.Helper()

	lines := strings.Split(document, "\n")

	var mutations []depguardMutation

	// counts says how many mutations each kind of weakening made, for the
	// log line at the end.
	counts := map[string]int{}

	add := func(m depguardMutation, kind string) {
		t.Helper()

		if strings.Join(m.lines, "\n") == document {
			t.Fatalf("%s: the mutation left .golangci.yml unchanged, so its case would pass on nothing", m.name)
		}

		counts[kind]++
		mutations = append(mutations, m)
	}

	for _, rule := range pinnedDepguardRules {
		span := findDepguardRule(t, lines, rule.Name)

		// The entries are read from the document, not from the pin: a rule
		// that shares its deny list through an alias has no entry lines of
		// its own, and the rule it shares them with is mutated on its own.
		sections := make(map[string][]depguardEntry, 3)

		for _, section := range []string{"files", "allow", "deny"} {
			sections[section] = depguardEntries(t, lines, span, section)

			want := len(rule.Files)

			switch section {
			case "allow":
				want = len(rule.Allow)
			case "deny":
				want = len(rule.Deny)
			}

			if depguardSectionAliased(t, lines, span, section) {
				want = 0
			}

			if got := len(sections[section]); got != want {
				t.Fatalf("%s: the document holds %d %s entries, the pin holds %d: the entry walk lost entries, so the mutations below would miss them", rule.Name, got, section, want)
			}
		}

		add(depguardMutation{
			name:  rule.Name + ": rule deleted",
			rule:  rule.Name,
			lines: deleteDepguardLines(lines, span.key, span.end),
			// The alias rule needs the anchor this rule holds: the reader
			// refuses the document, which is the check failing closed.
			wantParseError: rule.Name == "app-uses-ports-not-adapters",
		}, "rule deleted")

		for _, section := range []string{"files", "allow", "deny"} {
			for i, entry := range sections[section] {
				add(depguardMutation{
					name:  fmt.Sprintf("%s: %s entry %d deleted: %s", rule.Name, section, i, entry.text),
					rule:  rule.Name,
					lines: deleteDepguardLines(lines, entry.from, entry.to),
				}, section+" entry deleted")
			}
		}

		for i, entry := range sections["allow"] {
			if !entry.exact() {
				continue
			}

			mutated := slices.Clone(lines)
			mutated[entry.from] = dropDollar(lines[entry.from])

			add(depguardMutation{
				name:  fmt.Sprintf("%s: allow entry %d without its $: %s", rule.Name, i, entry.text),
				rule:  rule.Name,
				lines: mutated,
			}, "allow entry dropped its $")
		}

		// An addition weakens a rule as surely as a deletion: an allow
		// entry, a files exclusion or a laxer list-mode is what a careless
		// edit adds.
		if at, ok := depguardInsertPoint(t, lines, span, "allow"); ok {
			add(depguardMutation{
				name:  rule.Name + ": allow entry added: os",
				rule:  rule.Name,
				lines: insertDepguardLine(blockEmptyFlowSection(t, lines, span, "allow"), at, 12, "- os"),
			}, "allow entry added")
		}

		if at, ok := depguardInsertPoint(t, lines, span, "files"); ok {
			add(depguardMutation{
				name:  rule.Name + `: files entry added: "!**/x/**"`,
				rule:  rule.Name,
				lines: insertDepguardLine(lines, at, 12, `- "!**/x/**"`),
			}, "files entry added")
		}

		if at, mode, ok := depguardListMode(t, lines, span); ok {
			for _, other := range []string{"original", "strict", "lax"} {
				if other == mode {
					continue
				}

				mutated := slices.Clone(lines)
				mutated[at] = "          list-mode: " + other

				add(depguardMutation{
					name:  fmt.Sprintf("%s: list-mode rewritten from %s to %s", rule.Name, mode, other),
					rule:  rule.Name,
					lines: mutated,
				}, "list-mode rewritten")
			}

			add(depguardMutation{
				name:  rule.Name + ": list-mode line deleted",
				rule:  rule.Name,
				lines: deleteDepguardLines(lines, at, at+1),
			}, "list-mode deleted")
		} else {
			add(depguardMutation{
				name:  rule.Name + ": list-mode strict added",
				rule:  rule.Name,
				lines: insertDepguardLine(lines, span.key+1, 10, "list-mode: strict"),
			}, "list-mode added")
		}
	}

	if len(mutations) == 0 {
		t.Fatal("no mutation was made, so this test would prove nothing")
	}

	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}

	slices.Sort(kinds)

	breakdown := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		breakdown = append(breakdown, fmt.Sprintf("%d %s", counts[kind], kind))
	}

	t.Logf("%d mutations of .golangci.yml: %s", len(mutations), strings.Join(breakdown, ", "))

	return mutations
}

// depguardRuleSpan is where one rule sits in the .golangci.yml lines: its key
// line and the last line of its body.
type depguardRuleSpan struct {
	key int
	// end is one past the rule's last line.
	end int
}

// depguardEntry is one entry line of a rule's files, allow or deny list, from
// its dashes of the line it starts on to the last line that belongs to it: the
// desc of a deny entry is on the line after its pkg.
type depguardEntry struct {
	text string
	from int
	to   int
}

// indentOf returns the indentation of a line and its text, trailing white space
// removed.
func indentOf(line string) (int, string) {
	text := strings.TrimLeft(line, " ")
	spaces := len(line) - len(text)
	trimmed := strings.TrimRight(text, " \t")

	return spaces, trimmed
}

// isDepguardContent reports whether a line holds something but a comment.
func isDepguardContent(text string) bool {
	return text != "" && !strings.HasPrefix(text, "#")
}

// findDepguardRule returns where the rule named name sits in the lines of
// .golangci.yml. A rule is a key at the indentation of the depguard rules, and
// its body is every line below it indented further; the test fails when the
// rule cannot be found, rather than mutate nothing.
func findDepguardRule(t *testing.T, lines []string, name string) depguardRuleSpan {
	t.Helper()

	for i, line := range lines {
		indent, text := indentOf(line)
		if indent != 8 || text != name+":" {
			continue
		}

		end := len(lines)

		for j := i + 1; j < len(lines); j++ {
			jIndent, jText := indentOf(lines[j])
			if isDepguardContent(jText) && jIndent <= indent {
				end = j

				break
			}
		}

		return depguardRuleSpan{key: i, end: end}
	}

	t.Fatalf("no depguard rule named %s in .golangci.yml, so this mutation test would pass on nothing", name)

	return depguardRuleSpan{}
}

// The helpers below read the mutated .golangci.yml by indentation and assume
// its 2-space layout: the depguard rules at indent 8, their keys at 10 and the
// entries of a list at 12. They fail, naming the rule and the section, when the
// document no longer has that shape, rather than mutate nothing.

// depguardSectionKey returns the line of the rule's section key (files, allow
// or deny) and whether the rule has it.
func depguardSectionKey(t *testing.T, lines []string, span depguardRuleSpan, section string) (int, bool) {
	t.Helper()

	for i := span.key + 1; i < span.end; i++ {
		indent, text := indentOf(lines[i])
		if indent == 10 && (text == section+":" || strings.HasPrefix(text, section+": ")) {
			return i, true
		}
	}

	return 0, false
}

// depguardSectionAliased reports whether the rule's section key stands for a
// list written under another rule (deny: *app-deny): the section has no entry
// lines of its own, and the rule whose list it is holds them.
func depguardSectionAliased(t *testing.T, lines []string, span depguardRuleSpan, section string) bool {
	t.Helper()

	key, ok := depguardSectionKey(t, lines, span, section)
	if !ok {
		return false
	}

	_, text := indentOf(lines[key])
	value := strings.TrimSpace(strings.TrimPrefix(text, section+":"))

	return strings.HasPrefix(value, "*")
}

// depguardEntries returns the entry lines of the rule's section (files, allow
// or deny), in file order. A section the rule does not have, or one whose list
// it shares through an alias, has no entry line of its own.
func depguardEntries(t *testing.T, lines []string, span depguardRuleSpan, section string) []depguardEntry {
	t.Helper()

	key, ok := depguardSectionKey(t, lines, span, section)
	if !ok {
		return nil
	}

	var entries []depguardEntry

	for i := key + 1; i < span.end; {
		indent, text := indentOf(lines[i])
		if !isDepguardContent(text) {
			i++

			continue
		}

		if indent != 12 || !strings.HasPrefix(text, "- ") {
			break
		}

		to := i + 1

		for j := i + 1; j < span.end; j++ {
			jIndent, jText := indentOf(lines[j])
			if !isDepguardContent(jText) {
				continue
			}

			if jIndent <= indent {
				break
			}

			to = j + 1
		}

		entries = append(entries, depguardEntry{text: text[2:], from: i, to: to})

		// Resume after the entry's last line: the desc of a deny entry is
		// on the line after its pkg, and reading it as the next entry would
		// end the walk at the first entry of the list.
		i = to
	}

	return entries
}

// depguardInsertPoint returns the line a new entry of the rule's section
// belongs before: after the section's last entry, or right after the key when
// the list has none. It reports false for a section the rule does not have and
// for one it shares through an alias, which has no entries to add to.
func depguardInsertPoint(t *testing.T, lines []string, span depguardRuleSpan, section string) (int, bool) {
	t.Helper()

	key, ok := depguardSectionKey(t, lines, span, section)
	if !ok || depguardSectionAliased(t, lines, span, section) {
		return 0, false
	}

	if entries := depguardEntries(t, lines, span, section); len(entries) > 0 {
		return entries[len(entries)-1].to, true
	}

	return key + 1, true
}

// blockEmptyFlowSection returns the lines with the rule's section key rewritten
// from an empty flow sequence (allow: [], a rule that allows nothing, issue
// #166) to a bare key, so an entry inserted below it is a block sequence entry
// a careless edit could add, not a key with a value and lines below it, which
// no reader takes. Any other section comes back as it is.
func blockEmptyFlowSection(t *testing.T, lines []string, span depguardRuleSpan, section string) []string {
	t.Helper()

	key, ok := depguardSectionKey(t, lines, span, section)
	if !ok {
		return lines
	}

	_, text := indentOf(lines[key])
	if strings.TrimSpace(strings.TrimPrefix(text, section+":")) != "[]" {
		return lines
	}

	out := slices.Clone(lines)
	out[key] = strings.Repeat(" ", 10) + section + ":"

	return out
}

// depguardListMode returns the line of the rule's list-mode key and the mode it
// writes, and whether the rule writes one.
func depguardListMode(t *testing.T, lines []string, span depguardRuleSpan) (int, string, bool) {
	t.Helper()

	for i := span.key + 1; i < span.end; i++ {
		indent, text := indentOf(lines[i])
		if indent == 10 && strings.HasPrefix(text, "list-mode: ") {
			return i, strings.TrimSpace(strings.TrimPrefix(text, "list-mode:")), true
		}
	}

	return 0, "", false
}

// insertDepguardLine returns the lines with text inserted before line at,
// indented by indent spaces.
func insertDepguardLine(lines []string, at, indent int, text string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, strings.Repeat(" ", indent)+text)
	out = append(out, lines[at:]...)

	return out
}

// exact reports whether the entry ends the way an exact depguard entry does.
func (e depguardEntry) exact() bool {
	return endsInDollar(e.text)
}

// endsInDollar reports whether a scalar entry ends in $, its quotes counted as
// the quotes they are.
func endsInDollar(text string) bool {
	return strings.HasSuffix(unquote(text), "$")
}

// unquote returns the text with the quotes a YAML scalar would have around it
// removed.
func unquote(text string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), `"`), `"`)
}

// dropDollar returns the entry's line with the trailing $ of its value dropped,
// its quotes and indentation kept: the weakening the pin must notice.
func dropDollar(line string) string {
	text := strings.TrimRight(line, " ")
	quote := ""

	if strings.HasSuffix(text, `"`) {
		quote = `"`

		text = strings.TrimSuffix(text, `"`)
	}

	return strings.TrimSuffix(strings.TrimSuffix(text, "$"), `"`) + quote
}

// deleteDepguardLines returns the lines without [from, to).
func deleteDepguardLines(lines []string, from, to int) []string {
	out := make([]string, 0, len(lines)-(to-from))
	out = append(out, lines[:from]...)
	out = append(out, lines[to:]...)

	return out
}

// requireMutationReported fails unless the mutated document makes PinnedRules
// report a difference that starts with the rule's name, or, for the mutation
// marked as breaking the document, is refused by the reader.
// depguardMutations has already refused a mutation that changes nothing.
func requireMutationReported(t *testing.T, mutation depguardMutation) {
	t.Helper()

	rules, err := check.DepguardRules([]byte(strings.Join(mutation.lines, "\n")))
	if err != nil {
		if !mutation.wantParseError {
			t.Fatalf("the mutated document is refused with %v, and this is not a mutation the reader may refuse: a parse error here hides the weakening rather than reporting it", err)
		}

		return
	}

	if mutation.wantParseError {
		t.Fatalf("the mutated document reads as %v, and this mutation removes what the rule below it needs: the reader must refuse it, not read a weaker rule", rules)
	}

	if !slices.ContainsFunc(check.PinnedRules(rules, pinnedDepguardRules), func(message string) bool {
		return strings.HasPrefix(message, mutation.rule+": ")
	}) {
		t.Errorf("the mutated document reads as the pinned rules: DepguardRules() = %v and PinnedRules() reported nothing about %s, so the pin would miss this mutation", rules, mutation.rule)
	}
}

// appTestsExtras are the standard packages app-tests-use-fakes-not-adapters
// allows beyond the ones domain-is-pure names, each with the reason the
// application layer's tests give for it (issue #144). The list is deliberately
// short: the rule is a strict allow list holding tests that use fakes, so a
// package here is one a test cannot do without, not one a test might find
// convenient. A package outside this list and outside domain-is-pure's is
// refused by the rule, and TestAppTestsRuleAllowsOnlyPurePackagesAndItsExtras
// fails when .golangci.yml allows one.
var appTestsExtras = []struct {
	path   string
	reason string
}{
	{path: "bytes", reason: "a test compares what the use case returned, byte for byte"},
	{path: "context", reason: "the use case takes a context.Context, so a test passes one"},
	{path: "io", reason: "a test writes through an io.Writer, which is how a use case hands output to a port"},
	{path: "sync", reason: "a fake records what it was handed behind a sync.Mutex, as two goroutines may reach it"},
	{path: "testing", reason: "a test is written with package testing"},
}

// appTestsThirdParty is the one third-party package
// app-tests-use-fakes-not-adapters allows: the comparison the testing section of
// CLAUDE.md asks for. It is no standard package, so the check below names it
// rather than compare it with the domain's list.
const appTestsThirdParty = "github.com/google/go-cmp/cmp"

// TestAppTestsRuleAllowsOnlyPurePackagesAndItsExtras pins that the application
// layer's test rule allows no standard package beyond the two sets it is built
// from (issue #144, the reviewer's finding B1 and the auditor's L-2): the
// packages domain-is-pure names, whose list the app-tests rule shares so the
// domain and the tests of its caller agree on what pure means, and the extras
// above, each of which a test of the layer gives a reason for.
//
// It is a subset check, not a superset one: the rule need not allow every
// package the domain allows. fmt and time are the domain's, and a test's copy
// of them would print to stdout, read stdin or read zoneinfo files and the
// clock, which no forbidigo group refuses in a test (its groups hold for
// internal/domain's production files). The other direction is the pin's job:
// pinnedDepguardRules holds the rule's allow list whole, so a package the tests
// need that is dropped from this list fails TestDepguardRulesArePinned, and the
// imports of the layer's tests fail TestReleaseTargetDependencies too.
//
// The entries compared are the rule's standard packages: every allow entry that
// is neither a package of the module nor the one third-party package. Each must
// be an exact entry — a prefix entry would allow every package under it and
// would let a look-alike in — and each must be one the domain allows or one of
// the extras.
//
// So that the extras cannot go stale, each of them must also be an exact entry of
// the rule's allow list and no entry of domain-is-pure: an extra a package of the
// layer stops importing, or one the domain's list starts to name, is dropped from
// appTestsExtras in the same commit.
func TestAppTestsRuleAllowsOnlyPurePackagesAndItsExtras(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	pure, err := check.AllowList(golangci, "domain-is-pure")
	if err != nil {
		t.Fatalf("AllowList(.golangci.yml, domain-is-pure): %v", err)
	}

	tests, err := check.AllowList(golangci, "app-tests-use-fakes-not-adapters")
	if err != nil {
		t.Fatalf("AllowList(.golangci.yml, app-tests-use-fakes-not-adapters): %v", err)
	}

	purePaths := make(map[string]bool, len(pure))

	for _, entry := range pure {
		if entry.Prefix {
			t.Errorf("domain-is-pure allows %s by prefix, and a prefix entry allows every package under it: write %s$ for the package the domain may import", entry.Path, entry.Path)

			continue
		}

		purePaths[entry.Path] = true
	}

	extras := make(map[string]bool, len(appTestsExtras))

	for _, extra := range appTestsExtras {
		if extra.reason == "" {
			t.Errorf("appTestsExtras names %s with no reason: say why the application layer's tests need it", extra.path)
		}

		extras[extra.path] = true
	}

	var (
		checked int
		refused []string
	)

	for _, entry := range tests {
		// The module's own packages and the one third-party package are not
		// the standard library's: which of them a test may import is its own
		// question, and the pin answers it.
		if strings.HasPrefix(entry.Path, modulePath+"/") || entry.Path == appTestsThirdParty {
			continue
		}

		checked++

		if entry.Prefix {
			refused = append(refused, entry.Path+" (a prefix entry, not an exact one)")

			continue
		}

		if purePaths[entry.Path] || extras[entry.Path] {
			continue
		}

		refused = append(refused, entry.Path+"$")
	}

	if checked == 0 {
		t.Fatal("app-tests-use-fakes-not-adapters allows no standard package, so this test would check nothing")
	}

	if len(refused) != 0 {
		slices.Sort(refused)

		names := make([]string, 0, len(appTestsExtras))
		for _, extra := range appTestsExtras {
			names = append(names, extra.path)
		}

		t.Errorf("app-tests-use-fakes-not-adapters allows %s, which domain-is-pure does not name and appTestsExtras does not hold (it holds %s): the application layer's tests may import the domain's pure packages and those extras and nothing else; drop the entry from .golangci.yml and pinnedDepguardRules in the same commit, or add it to appTestsExtras with the reason a test needs it",
			strings.Join(refused, ", "), strings.Join(names, ", "))
	}

	// The other direction, so the extras cannot go stale: an extra the rule no
	// longer allows is one the layer's tests stopped importing, and one the
	// domain's list names is no extra at all.
	allowed := make(map[string]bool, len(tests))

	for _, entry := range tests {
		if !entry.Prefix {
			allowed[entry.Path] = true
		}
	}

	for _, extra := range appTestsExtras {
		if !allowed[extra.path] {
			t.Errorf("appTestsExtras names %s, which is no exact allow entry of app-tests-use-fakes-not-adapters: drop it from appTestsExtras, and from .golangci.yml and pinnedDepguardRules if the rule no longer allows it", extra.path)
		}

		if purePaths[extra.path] {
			t.Errorf("appTestsExtras names %s, which domain-is-pure already allows: it is no extra, so drop it from appTestsExtras", extra.path)
		}
	}
}
