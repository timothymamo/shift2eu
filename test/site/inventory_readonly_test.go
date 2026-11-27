package site_test

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file holds the guide-wide read-only check of the inventory guide (issue
// #716): every `aws`, `az` and `gcloud` command the guide tells a visitor to run
// must only read. CLAUDE.md rule 1 forbids writing to a cloud, and the guide's
// commands run against the visitor's own account with the visitor's own
// credentials, so a `create`, `delete` or `set` no check catches would be a write
// this project tells its readers to make.
//
// Each milestone's review held its new commands to a read verb by hand, and the
// inventory page's checks pin single commands by name. This check holds all of
// them at once, and it reads the whole guide rather than a list of sections: every
// fenced block whose language is a shell one, every inline code span of the prose,
// every prose line that starts with a CLI name, and the comments of the programs
// the guide ships. A block whose language is neither a shell one nor on the closed
// list of prose languages fails the check, so no block of commands goes unread.
//
// An invocation's command path is held to the exact list of read commands its CLI
// has in cloudCLIs, not to a verb: a new command is a deliberate edit of that list.
// The words after the command path are the command's positional arguments, so
// `gcloud compute instances delete list` reads nothing and fails, and a read that
// returns a secret (`aws secretsmanager get-secret-value`) is no read the guide
// may run. A flag that points the CLI elsewhere or turns its checks off
// (`--endpoint-url`, `--no-verify-ssl`, `--ca-bundle`, `--impersonate-service-
// account`) fails even on a path that reads, and `az rest` reads only as one GET
// of the management endpoint, with no resource, headers or body, and with its
// method flag spelt out.
//
// Nothing here is trusted on its own. unplacedCLINames is the fail-closed backstop:
// it splits every line of the guide and of every program into fields on white space
// and shell punctuation, whatever the Markdown says the line is, and holds every
// field that is one of the three names to account, so a `bash -c` argument, a
// heredoc body, a blockquoted or indented block, an HTML block, a double-backtick
// or wrapped code span, and a write in a ```text fence are all reported.
//
// Residuals, the escapes this check knowingly leaves, named here in one place:
//
//   - A name the field scan never sees as one field: `a""ws`, `\a\w\s` and
//     `$'\x61ws'` spell a CLI's name in bytes no field equals.
//   - A command run by something the guide does not ship: a downloaded script, an
//     alias an earlier command defined, a `make` target whose recipe lives in a
//     Makefile. The check reads the guide's text and its programs.
//   - A redirect target: `> "$out/x.json"` stands where a program's name stands in
//     shell, so the check reads it as a file and never as a variable that could
//     name a CLI.
//   - An `az rest` that gives no address: the guide's prose names `az rest
//     --method get --url` with no value, and the programs' comment table names
//     `az rest --method get .../queues`, so only an address that is given is held
//     to the management endpoint, and the one method, get, is what decides.
//   - The backstop counts per line and in the line's order: a start is credited to
//     the first CLI name on its line, and a field sequence that reads accounts for
//     a name the extraction could not place, so a read named inside a `bash -c`
//     argument passes (it is a read) and a bare mention needs no start.
//   - Variable indirection is refused only where a program's name stands: `"$cli"
//     ec2 create-tags` fails, while `xargs "$cli" ec2 create-tags` is an argument,
//     and a `$`-word that expands to `aws` from a variable the guide never sets is
//     invisible to the check.
//   - CommonMark's indentation is counted in spaces: a tab before a fence counts as
//     one space here and as up to four columns there, and a fence inside a list
//     item is read at the guide's own indentation.
//   - A fence labelled with a prose language is read for CLI names alone, by the
//     backstop: a CLI name in one must be a bare mention, an invocation of the
//     extraction, or a read, so a write in a ```json sample fails while the
//     sample's other words are no shell the check reads.
//
// The check is a function over text rather than over the file, so
// TestReadOnlyCheckFlagsPlantedWrites can plant a write into a copy of the guide
// in memory and see the check report it: a check that never bites would pass the
// guide today and catch nothing tomorrow.

// inventoryGuidePath is the guide this file's checks read, relative to the
// repository root.
const inventoryGuidePath = "docs/guide/inventory.md"

// azREST is `az rest`, the one command whose read depends on flags.
const azREST = "rest"

// methodFlag is the long form of `az rest`'s HTTP method flag.
const methodFlag = "--method"

// azManagementAPI is the host an `az rest` read addresses the Azure Resource
// Manager API through: every address the guide gives begins with it, and the ones
// that carry a resource id continue with a `$` substitution.
const azManagementAPI = "https://management.azure.com"

// cloudCLIs are the three cloud CLIs the guide tells a visitor to run, each with
// the exact command paths its invocations may name, the flags no read may pass, and
// the command reference those paths come from, the source of every read command the
// list holds (issue #716's review: a verb rule let a write whose last word was a
// read verb through).
var cloudCLIs = []cloudCLI{{
	name: "aws",
	// AWS CLI version 2's command reference, service by service: the guide reads
	// only with the operations listed here, so `aws ec2 describe-instances` is a
	// read and `aws ec2 create-tags` is not, and neither is a read of a secret
	// (`secretsmanager get-secret-value`, `ecr get-login-password`).
	reference: "https://awscli.amazonaws.com/v2/documentation/api/latest/reference/index.html",
	readCommands: []string{
		"autoscaling describe-auto-scaling-groups",
		"configservice select-resource-config",
		"ec2 describe-instance-types",
		"ec2 describe-instances",
		"ec2 describe-snapshots",
		"ec2 describe-volumes",
		"efs describe-file-systems",
		"eks describe-cluster",
		"eks describe-nodegroup",
		"eks list-clusters",
		"eks list-nodegroups",
		"elb describe-load-balancers",
		"elasticache describe-cache-clusters",
		"elasticache describe-replication-groups",
		"elasticache describe-serverless-caches",
		"elbv2 describe-listeners",
		"elbv2 describe-load-balancers",
		"elbv2 describe-tags",
		"elbv2 describe-target-groups",
		"kafka list-clusters-v2",
		"lambda list-functions",
		"opensearch describe-domains",
		"opensearch list-domain-names",
		"opensearchserverless batch-get-collection",
		"opensearchserverless list-collections",
		"rds describe-db-instances",
		"route53 get-dnssec",
		"route53 list-hosted-zones",
		"route53 list-resource-record-sets",
		"s3api get-bucket-versioning",
		"s3api list-buckets",
		"sns get-topic-attributes",
		"sns list-topics",
		"sqs get-queue-attributes",
		"sqs list-queues",
	},
	// --endpoint-url sends the request to a host of the caller's choosing, and
	// --no-verify-ssl and --ca-bundle turn off the check that the answer comes from
	// AWS: a read that passes either reads something else's account.
	deniedFlags: []string{"--ca-bundle", "--endpoint-url", "--no-verify-ssl"},
}, {
	name: "az",
	// The Azure CLI's reference index: `az rest` reads only with one GET method on
	// the management endpoint, and no command that returns an account's keys or a
	// vault's secret is a read (`storage account keys list`, `keyvault secret
	// show`).
	reference: "https://learn.microsoft.com/en-us/cli/azure/reference-index",
	readCommands: []string{
		"account show",
		"graph query",
		azREST,
		"servicebus namespace list",
		"servicebus queue list",
		"servicebus topic list",
		"servicebus topic subscription list",
		"storage account blob-service-properties show",
		"storage account list",
		"vm list-sizes",
	},
	// `az rest --resource` addresses Microsoft Graph with the visitor's
	// management token, and --headers and --body carry a request of the caller's
	// choosing: no read passes any of them.
	deniedFlags: []string{"--body", "--headers", "--resource"},
}, {
	name: "gcloud",
	// The gcloud reference: every command the guide runs is a `list`, and
	// `asset search-all-resources` is its Cloud Asset Inventory read.
	reference: "https://cloud.google.com/sdk/gcloud/reference",
	readCommands: []string{
		"artifacts repositories list",
		"asset search-all-resources",
		"compute backend-services list",
		"compute disks list",
		"compute forwarding-rules list",
		"compute instance-groups managed list",
		"compute instances list",
		"compute machine-types list",
		"compute snapshots list",
		"compute target-http-proxies list",
		"compute target-https-proxies list",
		"compute target-pools list",
		"compute url-maps list",
		"container clusters list",
		"dns managed-zones list",
		"dns record-sets list",
		"filestore instances list",
		"functions list",
		"managed-kafka clusters list",
		"memorystore instances list",
		"pubsub subscriptions list",
		"redis clusters list",
		"redis instances list",
		"sql instances list",
		"storage buckets list",
	},
	// --impersonate-service-account makes the call as a service account the
	// visitor may not own a read of.
	deniedFlags: []string{"--impersonate-service-account"},
}}

// cloudCLI is one of the cloud CLIs the guide runs.
type cloudCLI struct {
	// name is the program's name: the word an invocation starts with, or its last
	// path element.
	name string
	// reference is the CLI's command reference, the source of readCommands.
	reference string
	// readCommands are the exact command paths the guide may run, each of them a
	// read: a path that starts with one of them reads, and one that starts with
	// none is no read.
	readCommands []string
	// deniedFlags are the flags no invocation of the CLI may pass, whatever its
	// command path: a read that points the CLI elsewhere or turns its checks off is
	// no read the guide may run.
	deniedFlags []string
}

// cloudCLIByName returns the CLI the name stands for, and whether there is one. It
// is the only lookup: the failure messages run outside a test, and no caller can
// fail a test for a name that is not there.
func cloudCLIByName(name string) (cloudCLI, bool) {
	for _, cli := range cloudCLIs {
		if cli.name == name {
			return cli, true
		}
	}

	return cloudCLI{}, false
}

// reads returns the command path an invocation names, as the guide writes it, and
// whether the invocation is a read: the path must start with one of the CLI's read
// commands, no word may name a flag the CLI denies, and `az rest`, the one command
// whose read depends on flags, must be one GET of the management endpoint. The path
// it returns holds the word that made the invocation no read, so the message says
// where the check stopped.
func (c cloudCLI) reads(words []string) (string, bool) {
	commandPath := subcommandPath(words)
	held := strings.Join(commandPath, " ")

	for _, word := range words {
		if slices.Contains(c.deniedFlags, flagName(word)) {
			return strings.TrimSpace(held + " " + flagName(word)), false
		}
	}

	command, ok := c.readPrefix(commandPath)
	if !ok {
		return held, false
	}

	if command == azREST {
		return azRestReads(words)
	}

	return held, true
}

// azRestReads returns what an `az rest` invocation is held to, and whether it
// reads: exactly one method, get, and, when it gives an address, one on the
// management endpoint. An invocation that abbreviates the method flag — `-mpost`,
// `-m=post`, `--meth put` — is refused rather than read, since the check would
// otherwise take it for one with no method at all and let the write through.
func azRestReads(words []string) (string, bool) {
	var (
		methods []string
		address string
		urlFlag string
	)

	for i := 0; i < len(words); i++ {
		name, value, inline := strings.Cut(words[i], "=")

		switch {
		case words[i] == methodFlag || words[i] == "-m":
			if i+1 < len(words) {
				i++

				methods = append(methods, words[i])
			}
		case strings.HasPrefix(words[i], methodFlag+"="):
			methods = append(methods, value)
		case name == "--url" || name == "--uri" || name == "-u":
			if !inline && i+1 < len(words) {
				i++
				value = words[i]
			}

			address, urlFlag = value, name
		case isMethodAbbreviation(words[i]):
			return azREST + " " + words[i], false
		}
	}

	oneGet := len(methods) == 1 && strings.EqualFold(methods[0], "get")
	addressReads := address == "" || addressesManagement(address)

	if oneGet && addressReads {
		return azREST + " --method " + methods[0], true
	}

	held := azREST

	if len(methods) > 0 && !oneGet {
		held += " --method " + strings.Join(methods, " ")
	}

	if !addressReads {
		held += " " + urlFlag + " " + address
	}

	return held, false
}

// isMethodAbbreviation reports whether a word spells the method flag in a shape az
// accepts and this check would not read: a short flag with its value run on
// (`-mpost`, `-m=post`) or a truncation of `--method` (`--meth`), which az's own
// parser takes for the flag. `-m`, `--method` and `--method=<value>` are no
// abbreviations.
func isMethodAbbreviation(word string) bool {
	if word == "" || word == "-m" || strings.HasPrefix(word, methodFlag) {
		return false
	}

	// A short flag with its value run on, or a truncation az's parser completes.
	return strings.HasPrefix(word, "-m") || len(word) < len(methodFlag) && methodFlag[:len(word)] == word
}

// flagName returns the name of a word, the value it may carry left out:
// `--endpoint-url=https://example.com` names the flag --endpoint-url.
func flagName(word string) string {
	name, _, _ := strings.Cut(word, "=")

	return name
}

// addressesManagement reports whether an address is one of the management
// endpoint's: the host, then a path, a substitution, a query or nothing at all. A
// host whose name merely begins with the endpoint's —
// `https://management.azure.com.example.com/x` — is no address of it.
func addressesManagement(address string) bool {
	rest, ok := strings.CutPrefix(address, azManagementAPI)
	if !ok {
		return false
	}

	return rest == "" || strings.ContainsRune("/$?", rune(rest[0]))
}

// readPrefix returns the longest read command the invocation's command path starts
// with, and whether there is one. The words after that command are the command's
// positional arguments, so a command whose path holds no read command of the CLI
// — `gcloud compute instances delete list`, `az vm delete show` — starts with none
// and reads nothing, whatever its last word is.
func (c cloudCLI) readPrefix(commandPath []string) (string, bool) {
	longest := ""

	for _, command := range c.readCommands {
		prefix := strings.Fields(command)
		if len(prefix) > len(commandPath) || len(command) <= len(longest) {
			continue
		}

		if slices.Equal(commandPath[:len(prefix)], prefix) {
			longest = command
		}
	}

	return longest, longest != ""
}

// cloudCommand is one invocation of a cloud CLI in a text: the CLI, the words it
// is given, where it was found, and the line of that text its CLI's name is on.
type cloudCommand struct {
	// cli is the CLI's name: "aws", "az" or "gcloud".
	cli string
	// words are the words of the invocation, the CLI's own name left out.
	words []string
	// where names the file and the place in it the invocation was found.
	where string
	// line is the 1-based line of the text the CLI's name is on, in the file:
	// unplacedCLINames credits each of a line's invocations to that line.
	line int
}

// String renders the invocation as the guide writes it.
func (c cloudCommand) String() string {
	return strings.TrimSpace(c.cli + " " + strings.Join(c.words, " "))
}

// write is one cloud CLI invocation the read-only check refuses: where it was
// found, the command itself, and the words the check held to the read commands.
type write struct {
	where   string
	cli     string
	command string
	path    string
}

// String renders the write with where the read commands it should have used come
// from, for a failure message.
func (w write) String() string {
	cli, known := cloudCLIByName(w.cli)
	if !known {
		return fmt.Sprintf("%s: %s names the CLI %q, which this check does not know, so it cannot say it reads (CLAUDE.md rule 1)",
			w.where, w.command, w.cli)
	}

	return fmt.Sprintf("%s: %s is no read command: %q is neither one of the %d command paths cloudCLIs lists for %s, nor a flag it reads with (CLAUDE.md rule 1, the guide may only read): %s",
		w.where, w.command, w.path, len(cli.readCommands), cli.name, cli.reference)
}

// TestInventoryGuideCommandsAreReadOnly holds every `aws`, `az` and `gcloud`
// command of the inventory guide, and of the programs it ships, to its CLI's read
// commands (issue #716, CLAUDE.md rule 1). The floors below are what keeps the
// check from passing over an extraction that found nothing: each CLI's commands
// are in the guide today, and a parser that quietly lost them fails here.
func TestInventoryGuideCommandsAreReadOnly(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, inventoryGuidePath))

	commands, problems := guideTextFindings(guide)
	for _, problem := range problems {
		t.Error(problem)
	}

	programs := shippedPrograms(t)

	for _, program := range programs {
		commands = append(commands, program.commands...)
	}

	// The guide holds 36 aws, 21 az and 25 gcloud invocations today, and the
	// programs' comments name 10 az (azure.jq's header table) and 1 gcloud
	// (gcp.jq's first paragraph); aws.jq's header names the operations without the
	// CLI's name, so it adds none. The check therefore holds 36 aws, 31 az and 26
	// gcloud invocations. They include the one aws command the guide has twice:
	// `ec2 describe-instance-types`, which it splits by `xargs -n 100` and
	// `xargs -n 1`. The floors are below those counts, so the guide may lose a
	// command without failing here, while an extraction that broke fails at once.
	floors := []struct {
		cli     string
		atLeast int
	}{
		{cli: "aws", atLeast: 33},
		{cli: "az", atLeast: 28},
		{cli: "gcloud", atLeast: 23},
	}

	for _, floor := range floors {
		found := 0

		for _, command := range commands {
			if command.cli == floor.cli {
				found++
			}
		}

		if found < floor.atLeast {
			t.Errorf("%s holds %d %s invocations, want at least %d, so this check read something: the extraction reads the guide's shell blocks, its prose and its programs, and a parser that finds nothing would pass every check below over an empty list",
				inventoryGuidePath, found, floor.cli, floor.atLeast)
		}
	}

	// A floor per program: aws.jq's comments name no whole command (their table
	// says `ec2 describe-instances`, without the CLI's name), so its floor is 0,
	// while azure.jq's and gcp.jq's commands must not vanish unnoticed.
	programFloors := []struct {
		program string
		atLeast int
	}{
		{program: guideProgramsDirectory + "aws.jq", atLeast: 0},
		{program: guideProgramsDirectory + "azure.jq", atLeast: 8},
		{program: guideProgramsDirectory + "gcp.jq", atLeast: 1},
	}

	for _, floor := range programFloors {
		found := -1

		for _, program := range programs {
			if program.path == floor.program {
				found = len(program.commands)
			}
		}

		if found < 0 {
			t.Errorf("the programs hold no %s, want it read here: a program the guide ships names commands too", floor.program)

			continue
		}

		if found < floor.atLeast {
			t.Errorf("%s's comments name %d cloud CLI invocations, want at least %d: the extraction reads a comment line that holds a CLI name anywhere, and one that stopped reading them would pass every check below",
				floor.program, found, floor.atLeast)
		}
	}

	for _, w := range notReadOnly(commands) {
		t.Error(w.String())
	}
}

// TestReadOnlyCheckFlagsPlantedWrites shows the check bites (issue #716): a write
// planted into a copy of the guide, in memory, is reported and the rest of the
// guide is not. Each case plants its command in one of the shapes the extraction
// must read — a duplicate heading, before the first heading, a ```sh fence, a
// ~~~bash fence, an inline code span of the prose, a prose line of its own — so a
// shape the extraction skipped is a case that reports no write. The cases after
// them plant the shapes the backstop and the flag rules must catch: a command
// hidden from the extraction, and a read that a flag turns into a write. The last
// case plants a fence label the check does not know, which must fail the reading
// rather than hide its block.
func TestReadOnlyCheckFlagsPlantedWrites(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, inventoryGuidePath))

	const (
		// A planted block lands in the guide's last section, which is this one.
		lastSection      = "Validate the result"
		lastSectionWhere = inventoryGuidePath + `, section "` + lastSection + `"`
		// A block above the guide's first heading has no section.
		beforeTheFirstHeadingWhere = inventoryGuidePath + ", before the first heading"
	)

	// The aws write every shape of fence and prose plants.
	const awsWrite = "aws ec2 create-tags --resources i-0123456789abcdef0 --tags Key=Name,Value=shop-web"

	fence := func(open, close, command string) string {
		return guide + "\n\n" + open + "\n" + command + "\n" + close + "\n"
	}

	// plantedLine is the line the text appended below starts on, and appended
	// returns the guide with lines of text of its own after its last section.
	plantedLine := len(strings.Split(guide, "\n")) + 2

	appended := func(lines ...string) string {
		return guide + "\n\n" + strings.Join(lines, "\n") + "\n"
	}

	// at names the backstop's problem for a line: the check that does not depend on
	// the Markdown or the shell reading names the line and the CLI it cannot place.
	at := func(line int, cli string) string {
		return fmt.Sprintf("line %d names the CLI %q", line, cli)
	}

	tests := []struct {
		name string
		// mutated is the guide with the case's plant.
		mutated string
		// want is the reading's whole set of writes.
		want []write
		// wantProblems are the substrings the reading must report among its
		// problems: a plant the reading itself must refuse.
		wantProblems []string
	}{{
		name:    "a planted aws write",
		mutated: fence("```bash", "```", "aws ec2 create-tags --resources i-0123456789abcdef0 --tags Key=Name,Value=shop-web"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a planted az write",
		mutated: fence("```bash", "```", "az group delete --name shop-web --yes"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az group delete --name shop-web --yes",
			path:    "group delete",
		}},
	}, {
		name:    "a planted gcloud write",
		mutated: fence("```bash", "```", "gcloud compute instances delete shop-web --zone=europe-west1-b --quiet"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "gcloud",
			command: "gcloud compute instances delete shop-web --zone=europe-west1-b --quiet",
			path:    "compute instances delete shop-web",
		}},
	}, {
		name:    "a planted az rest that is no GET",
		mutated: fence("```bash", "```", "az rest --method post --url https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/shop-web"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az rest --method post --url https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/shop-web",
			path:    "rest --method post",
		}},
	}, {
		name:    "a planted az rest that passes two methods",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --method post"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az rest --method get --url https://management.azure.com/x --method post",
			path:    "rest --method get post",
		}},
	}, {
		name:    "a write under a duplicate heading",
		mutated: guide + "\n\n## " + lastSection + "\n\n```bash\n" + awsWrite + "\n```\n",
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a write before the first heading",
		mutated: "```bash\n" + awsWrite + "\n```\n\n" + guide,
		want: []write{{
			where:   beforeTheFirstHeadingWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a write in a sh fence",
		mutated: fence("```sh", "```", awsWrite),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a write in a tilde fence",
		mutated: fence("~~~bash", "~~~", awsWrite),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a write in an inline code span",
		mutated: guide + "\n\nTag the instance by hand: `" + awsWrite + "`.\n",
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a write in a prose line",
		mutated: guide + "\n\n" + awsWrite + "\n",
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a gcloud positional argument that ends in a read word",
		mutated: fence("```bash", "```", "gcloud compute instances delete list"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "gcloud",
			command: "gcloud compute instances delete list",
			path:    "compute instances delete list",
		}},
	}, {
		name:    "an az positional argument that ends in a read word",
		mutated: fence("```bash", "```", "az vm delete show --name shop-web"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az vm delete show --name shop-web",
			path:    "vm delete show",
		}},
	}, {
		name:    "an aws read that returns a secret",
		mutated: fence("```bash", "```", "aws secretsmanager get-secret-value --secret-id shop-db"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: "aws secretsmanager get-secret-value --secret-id shop-db",
			path:    "secretsmanager get-secret-value",
		}},
	}, {
		// The plants below are for the backstop: a CLI name the Markdown and shell
		// reading cannot place fails the check wherever it sits, so no shape of the
		// guide can hide a write behind one the extraction skipped.
		name:         "a write in a bash -c argument",
		mutated:      appended(`bash -c 'aws --region eu-west-1 ec2 create-tags --resources i-0123456789abcdef0'`),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a bash -c argument a path names",
		mutated:      appended(`bash -c '/usr/local/bin/aws ec2 create-tags --resources i-0123456789abcdef0'`),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a bash -c argument after two spaces",
		mutated:      appended(`bash -c 'aws  ec2 create-tags --resources i-0123456789abcdef0'`),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a bash -c argument after a tab",
		mutated:      appended("bash -c 'aws\tec2 create-tags --resources i-0123456789abcdef0'"),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a bash -c argument that quotes its command group",
		mutated:      appended(`bash -c 'aws "ec2" create-tags --resources i-0123456789abcdef0'`),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in the body of a bash heredoc",
		mutated:      appended("```bash", "bash <<'EOF'", awsWrite, "EOF", "```"),
		wantProblems: []string{at(plantedLine+2, "aws")},
	}, {
		name:         "a write in the body of a heredoc piped to sh",
		mutated:      appended("```bash", "cat <<EOF | sh", awsWrite, "EOF", "```"),
		wantProblems: []string{at(plantedLine+2, "aws")},
	}, {
		name:         "a write in a blockquoted fence",
		mutated:      appended("> ```bash", "> "+awsWrite, "> ```"),
		wantProblems: []string{at(plantedLine+1, "aws")},
	}, {
		name:         "a write in an indented block behind sudo",
		mutated:      appended("    sudo " + awsWrite),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in an indented block behind an environment assignment",
		mutated:      appended("    AWS_PROFILE=shop " + awsWrite),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in an HTML block",
		mutated:      appended("<pre><code>" + awsWrite + "</code></pre>"),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a text fence",
		mutated:      appended("```text", awsWrite, "```"),
		wantProblems: []string{at(plantedLine+1, "aws")},
	}, {
		name:         "a write in a json fence",
		mutated:      appended("```json", awsWrite, "```"),
		wantProblems: []string{at(plantedLine+1, "aws")},
	}, {
		name:         "a write in a code span of two backticks",
		mutated:      appended("Tag it by hand: ``" + awsWrite + "``."),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a code span that spans two lines",
		mutated:      appended("Tag it by hand: `aws ec2 create-tags --resources", "  i-0123456789abcdef0`."),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		name:         "a write in a code span of a heading",
		mutated:      appended("## Tag it with `" + awsWrite + "`"),
		wantProblems: []string{at(plantedLine, "aws")},
	}, {
		// CommonMark: a fence opens with at most three spaces of indent, and a
		// backtick fence's info string holds no backtick, so neither line opens a
		// fence that could swallow the block of commands after it.
		name:    "a fence indented four spaces swallows no write",
		mutated: appended("    ```text", "```bash", awsWrite, "```"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		name:    "a fence whose info string holds a backtick swallows no write",
		mutated: appended("```text `x`", "```bash", awsWrite, "```"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: awsWrite,
			path:    "ec2 create-tags",
		}},
	}, {
		// az rest: one GET is what decides, and an address, a resource, a body or a
		// method the check would not read hide a write in a read's clothes.
		name:    "an az rest that addresses another API",
		mutated: fence("```bash", "```", "az rest --method get --url https://example.com/x"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az rest --method get --url https://example.com/x",
			path:    "rest --url https://example.com/x",
		}},
	}, {
		name:    "an az rest that passes a resource",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --resource https://graph.microsoft.com"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az rest --method get --url https://management.azure.com/x --resource https://graph.microsoft.com",
			path:    "rest --resource",
		}},
	}, {
		name:    "an az rest whose method is written as a short flag with its value",
		mutated: fence("```bash", "```", "az rest -mpost --url https://management.azure.com/x"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az rest -mpost --url https://management.azure.com/x",
			path:    "rest -mpost",
		}},
	}, {
		name:    "an az rest whose method flag is truncated",
		mutated: fence("```bash", "```", "az rest --meth put --url https://management.azure.com/x"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "az",
			command: "az rest --meth put --url https://management.azure.com/x",
			path:    "rest --meth",
		}},
	}, {
		// A read that points the CLI somewhere else or turns its checks off writes
		// to a cloud even where its command path reads.
		name:    "an aws read that names another endpoint",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --endpoint-url https://example.com"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: "aws ec2 describe-instances --endpoint-url https://example.com",
			path:    "ec2 describe-instances --endpoint-url",
		}},
	}, {
		name:    "an aws read that skips certificate verification",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --no-verify-ssl"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: "aws ec2 describe-instances --no-verify-ssl",
			path:    "ec2 describe-instances --no-verify-ssl",
		}},
	}, {
		name:    "an aws read that trusts another certificate bundle",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --ca-bundle ./ca.pem"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "aws",
			command: "aws ec2 describe-instances --ca-bundle ./ca.pem",
			path:    "ec2 describe-instances --ca-bundle",
		}},
	}, {
		name:    "a gcloud read that impersonates a service account",
		mutated: fence("```bash", "```", "gcloud compute instances list --impersonate-service-account sa@example-project.iam.gserviceaccount.com"),
		want: []write{{
			where:   lastSectionWhere,
			cli:     "gcloud",
			command: "gcloud compute instances list --impersonate-service-account sa@example-project.iam.gserviceaccount.com",
			path:    "compute instances list --impersonate-service-account",
		}},
	}, {
		name:         "a CLI a variable names",
		mutated:      fence("```bash", "```", `cli=aws; "$cli" ec2 create-tags --resources i-0123456789abcdef0`),
		wantProblems: []string{`"$cli"`},
	}, {
		name:         "a fence label the check does not know",
		mutated:      fence("```powershell", "```", "Get-EC2Instance -Region eu-central-1"),
		wantProblems: []string{`"powershell"`},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			commands, problems := guideTextFindings(tc.mutated)

			var gotProblems []string

			for _, want := range tc.wantProblems {
				for _, problem := range problems {
					if strings.Contains(problem, want) {
						gotProblems = append(gotProblems, want)

						break
					}
				}
			}

			if diff := cmp.Diff(tc.wantProblems, gotProblems); diff != "" {
				t.Errorf("the reading of the guide with %s reports problems other than the ones it must (-want +got):\n%s\nproblems: %v",
					tc.name, diff, problems)
			}

			if diff := cmp.Diff(tc.want, notReadOnly(commands), cmp.AllowUnexported(write{})); diff != "" {
				t.Errorf("the read-only check over the guide with %s differs from the writes it holds (-want +got):\n%s",
					tc.name, diff)
			}
		})
	}
}

// TestCloudReadDecisions pins the decision each CLI's read commands decide: the
// words the check holds an invocation to and whether it reads. The lists
// themselves are cloudCLIs, the one place a read command is written down, so a new
// guide command is a single edit there. The cases around a read are the ones the
// lists must refuse: a positional argument that ends in a read word, a read that
// returns a secret, a flag that points the CLI elsewhere, and `az rest` with one
// method, with GET, with two, with an abbreviated method flag and with an address
// beyond the management endpoint.
func TestCloudReadDecisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cli   string
		words []string
		verb  string
		read  bool
	}{
		{cli: "aws", words: []string{"ec2", "describe-instances"}, verb: "ec2 describe-instances", read: true},
		{cli: "aws", words: []string{"opensearchserverless", "batch-get-collection", "--ids"}, verb: "opensearchserverless batch-get-collection", read: true},
		{cli: "aws", words: []string{"configservice", "select-resource-config", "--expression", "SELECT 1"}, verb: "configservice select-resource-config", read: true},
		{cli: "aws", words: []string{"ec2", "describe-instances", "--output", "json"}, verb: "ec2 describe-instances", read: true},
		{cli: "aws", words: []string{"ec2", "describe-instances", "--endpoint-url", "https://example.com"}, verb: "ec2 describe-instances --endpoint-url"},
		{cli: "aws", words: []string{"ec2", "describe-instances", "--no-verify-ssl"}, verb: "ec2 describe-instances --no-verify-ssl"},
		{cli: "aws", words: []string{"s3api", "list-buckets", "--ca-bundle=./ca.pem"}, verb: "s3api list-buckets --ca-bundle"},
		{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, verb: "ec2 create-tags"},
		{cli: "aws", words: []string{"ec2", "delete-volume", "--volume-id", "vol-0123456789abcdef0"}, verb: "ec2 delete-volume"},
		{cli: "aws", words: []string{"ec2"}, verb: "ec2"},
		{cli: "aws", words: []string{"configservice", "select-aggregate-resource-config"}, verb: "configservice select-aggregate-resource-config"},
		{cli: "aws", words: []string{"secretsmanager", "get-secret-value", "--secret-id", "shop-db"}, verb: "secretsmanager get-secret-value"},
		{cli: "aws", words: []string{"ecr", "get-login-password"}, verb: "ecr get-login-password"},
		{cli: "az", words: []string{"account", "show", "--query", "id"}, verb: "account show", read: true},
		{cli: "az", words: []string{"servicebus", "topic", "subscription", "list", "--namespace-name", "x"}, verb: "servicebus topic subscription list", read: true},
		{cli: "az", words: []string{"graph", "query", "--graph-query", "Resources"}, verb: "graph query", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method=get", "--url=https://management.azure.com/x"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com.example.com/x"}, verb: "rest --url https://management.azure.com.example.com/x"},
		{cli: "az", words: []string{"rest", "--method", "GET", "--url", "https://management.azure.com/x"}, verb: "rest --method GET", read: true},
		{cli: "az", words: []string{"rest", "-m", "get", "--url", "https://management.azure.com/x"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--uri", "https://management.azure.com/x"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "-u", "https://management.azure.com/x"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--url"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://example.com/x"}, verb: "rest --url https://example.com/x"},
		{cli: "az", words: []string{"rest", "--method", "get", "--uri", "https://example.com/x"}, verb: "rest --uri https://example.com/x"},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x", "--resource", "https://graph.microsoft.com"}, verb: "rest --resource"},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x", "--headers", "x=y"}, verb: "rest --headers"},
		{cli: "az", words: []string{"rest", "--method", "post", "--body", "{}", "--url", "https://management.azure.com/x"}, verb: "rest --body"},
		{cli: "az", words: []string{"rest", "-mpost", "--url", "https://management.azure.com/x"}, verb: "rest -mpost"},
		{cli: "az", words: []string{"rest", "-m=post", "--url", "https://management.azure.com/x"}, verb: "rest -m=post"},
		{cli: "az", words: []string{"rest", "--meth", "put", "--url", "https://management.azure.com/x"}, verb: "rest --meth"},
		{cli: "az", words: []string{"rest", "--method=post", "--url", "https://management.azure.com/x"}, verb: "rest --method post"},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x", "--method", "post"}, verb: "rest --method get post"},
		{cli: "az", words: []string{"rest", "--url", "https://management.azure.com/x"}, verb: "rest"},
		{cli: "az", words: []string{"group", "delete", "--name", "shop-web"}, verb: "group delete"},
		{cli: "az", words: []string{"vm", "create", "--name", "shop-web"}, verb: "vm create"},
		{cli: "az", words: []string{"vm", "delete", "show"}, verb: "vm delete show"},
		{cli: "az", words: []string{"storage", "account", "keys", "list"}, verb: "storage account keys list"},
		{cli: "az", words: []string{"keyvault", "secret", "show"}, verb: "keyvault secret show"},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--project=x"}, verb: "compute instances list", read: true},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "shop-web"}, verb: "compute instances list shop-web", read: true},
		{cli: "gcloud", words: []string{"asset", "search-all-resources", "--scope=projects/x"}, verb: "asset search-all-resources", read: true},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--impersonate-service-account", "sa@example-project.iam.gserviceaccount.com"}, verb: "compute instances list --impersonate-service-account"},
		{cli: "gcloud", words: []string{"compute", "instances", "delete", "list"}, verb: "compute instances delete list"},
		{cli: "gcloud", words: []string{"compute", "instances", "delete", "shop-web"}, verb: "compute instances delete shop-web"},
		{cli: "gcloud", words: []string{"compute", "instances", "create", "shop-web"}, verb: "compute instances create shop-web"},
	}

	for _, tc := range tests {
		t.Run(tc.cli+" "+strings.Join(tc.words, " "), func(t *testing.T) {
			t.Parallel()

			cli, ok := cloudCLIByName(tc.cli)
			if !ok {
				t.Fatalf("%q is no CLI this check knows, want one of the guide's: %v", tc.cli, cloudCLIs)
			}

			verb, read := cli.reads(tc.words)

			if verb != tc.verb || read != tc.read {
				t.Errorf("%s %s reads as (%q, %t), want (%q, %t)",
					tc.cli, strings.Join(tc.words, " "), verb, read, tc.verb, tc.read)
			}
		})
	}
}

// TestCloudInvocationsFindEveryShape pins what the extraction finds (issue #716):
// an invocation after a pipe or xargs, inside `$(...)`, after `&&`, in a line
// joined by a line continuation, and in a quoted argument that spans lines, so a
// write in any of those shapes reaches the check. It pins the lines the invocations
// name — the backstop credits an invocation to the line its CLI's name is on — the
// shapes the check must refuse to read, and the two places shell text holds a
// program's name that are no command position: a redirect's target, and a word
// behind a path.
func TestCloudInvocationsFindEveryShape(t *testing.T) {
	t.Parallel()

	const where = "a command block"

	tests := []struct {
		name string
		text string
		want []cloudCommand
		// wantProblem is the substring of the problem the reading must report,
		// or "" for a text it reads.
		wantProblem string
	}{{
		name: "a command in a list piped to xargs",
		text: "jq -r '.DomainNames[].DomainName' \"$out/opensearch-domain-names.json\" |\n" +
			"  xargs -r -n 5 aws opensearch describe-domains --output json --domain-names\n",
		want: []cloudCommand{{cli: "aws", words: []string{"opensearch", "describe-domains", "--output", "json", "--domain-names"}, where: where, line: 2}},
	}, {
		name: "a command substitution inside a quoted word",
		text: `subscription="$(az account show --query id --output tsv)"`,
		want: []cloudCommand{{cli: "az", words: []string{"account", "show", "--query", "id", "--output", "tsv"}, where: where, line: 1}},
	}, {
		name: "a command in a backtick substitution inside a quoted word",
		text: "volumes=\"`aws ec2 create-tags --resources i-0123456789abcdef0`\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where, line: 1}},
	}, {
		name: "a command in a backtick substitution outside quotes",
		text: "out=`aws ec2 create-tags --resources i-0123456789abcdef0`",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where, line: 1}},
	}, {
		name: "a command reached through its path",
		text: "/usr/bin/aws ec2 create-tags --resources i-0123456789abcdef0",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where, line: 1}},
	}, {
		name: "a heredoc body that is no command",
		text: "cat > \"$out/notes.txt\" <<'EOF'\n" +
			"the zone's name is the visitor domain\n" +
			"EOF\n" +
			"aws ec2 create-tags --resources i-0123456789abcdef0\n",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where, line: 4}},
	}, {
		name: "a redirect's target is no program's name",
		text: "aws ec2 describe-volumes --output json > \"$out/ec2-volumes.json\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-volumes", "--output", "json"}, where: where, line: 1}},
	}, {
		name:        "a shell text that ends inside a quote",
		text:        "aws ec2 describe-volumes --query 'Volumes\n",
		wantProblem: "ends inside a single quote",
	}, {
		name:        "a shell text that ends inside a command substitution",
		text:        `out="$(aws ec2 describe-volumes`,
		wantProblem: "ends inside a command substitution",
	}, {
		name:        "a program's name a variable holds",
		text:        `cli=aws; "$cli" ec2 create-tags --resources i-0123456789abcdef0`,
		wantProblem: `"$cli"`,
	}, {
		name: "two commands joined by &&",
		text: "true && gcloud compute instances list --project=example-project && echo done",
		want: []cloudCommand{{cli: "gcloud", words: []string{"compute", "instances", "list", "--project=example-project"}, where: where, line: 1}},
	}, {
		name: "two commands joined by a semicolon",
		text: "aws ec2 describe-volumes; aws ec2 create-tags --resources i-0123456789abcdef0",
		want: []cloudCommand{
			{cli: "aws", words: []string{"ec2", "describe-volumes"}, where: where, line: 1},
			{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where, line: 1},
		},
	}, {
		name: "a command wrapped by a line continuation",
		text: "aws ec2 describe-snapshots --owner-ids self --output json \\\n" +
			"  --query '{Snapshots: Snapshots[].{VolumeId: VolumeId}}' > \"$out/ec2-snapshots.json\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-snapshots", "--owner-ids", "self", "--output", "json", "--query", "{Snapshots: Snapshots[].{VolumeId: VolumeId}}"}, where: where, line: 1}},
	}, {
		name: "a quoted filter that spans lines",
		text: "aws ec2 describe-volumes --output json --query '{Volumes: Volumes[].{\n" +
			"  VolumeId: VolumeId, Size: Size}}' \\\n" +
			"  > \"$out/ec2-volumes.json\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-volumes", "--output", "json", "--query", "{Volumes: Volumes[].{\n  VolumeId: VolumeId, Size: Size}}"}, where: where, line: 1}},
	}, {
		name: "a double-quoted argument whose braces and pipes are no operator",
		text: `az graph query --graph-query "Resources | where type =~ 'microsoft.compute/disks'"`,
		want: []cloudCommand{{cli: "az", words: []string{"graph", "query", "--graph-query", "Resources | where type =~ 'microsoft.compute/disks'"}, where: where, line: 1}},
	}, {
		name: "a comment that names a command",
		text: "# aws ec2 create-tags --resources i-0123456789abcdef0\naws ec2 describe-volumes\n",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-volumes"}, where: where, line: 2}},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, problems := invocationsIn(tc.text, true, where, 1)

			joined := strings.Join(problems, "\n")

			switch {
			case tc.wantProblem == "" && joined != "":
				t.Errorf("the reading of %q reports %q, want no problem: it reads the shape", tc.text, joined)
			case tc.wantProblem != "" && !strings.Contains(joined, tc.wantProblem):
				t.Errorf("the reading of %q reports %q, want a problem that holds %q: a command the check cannot place must not pass",
					tc.text, joined, tc.wantProblem)
			}

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(cloudCommand{})); diff != "" {
				t.Errorf("the invocations of %q differ from the ones it holds (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// TestProgramCommentsFindEveryShape pins what a program the guide ships is read
// for (issue #716): the comments' invocations — a CLI name anywhere in a comment
// line starts one, so `# then run: aws ec2 create-tags` is the write a line-start
// rule would miss, while a CLI name that follows a word is the comment's prose —
// and the backstop over every line of the program, which is what catches a command
// outside a comment.
func TestProgramCommentsFindEveryShape(t *testing.T) {
	t.Parallel()

	const program = "docs/guide/inventory/example.jq"

	tests := []struct {
		name string
		text string
		want []cloudCommand
		// wantWrites are the command paths the check refuses among want, nil
		// for a comment whose commands all read.
		wantWrites []string
		// wantProblems are the substrings the backstop must report.
		wantProblems []string
	}{{
		name: "a comment that starts with a command",
		text: "# aws ec2 describe-instances --output json\n",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-instances", "--output", "json"}, where: program + ", line 1", line: 1}},
	}, {
		name:       "a write a comment names after its prose",
		text:       "# then run: aws ec2 create-tags --resources i-0123456789abcdef0\n",
		want:       []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: program + ", line 1", line: 1}},
		wantWrites: []string{"ec2 create-tags"},
	}, {
		name: "a command a comment wraps across two lines",
		text: "#   {\"data\": ...}   az servicebus topic\n#                    subscription list\n",
		want: []cloudCommand{{cli: "az", words: []string{"servicebus", "topic", "subscription", "list"}, where: program + ", line 1", line: 1}},
	}, {
		name: "a command a comment follows with a comma and its prose",
		text: "# The listing (az graph query, one page) is the list of resources\n",
		want: []cloudCommand{{cli: "az", words: []string{"graph", "query"}, where: program + ", line 1", line: 1}},
	}, {
		name: "prose that names a CLI and no command",
		text: "# as gcloud writes a string), and never a command\n",
	}, {
		name: "a shape line that names an operation without the CLI",
		text: "#   {\"Buckets\": ...}   s3api list-buckets\n",
	}, {
		// azure.jq writes `az vm list-sizes` mid-prose three times today: no
		// comment command starts there, and the backstop places the words because
		// they read, so no list has to know the prose's command groups.
		name: "a read a comment names mid-prose",
		text: "# looked up in az vm list-sizes), a scale set's instance count\n",
	}, {
		name:         "a write a comment buries after a word",
		text:         "# and then aws ec2 create-tags --resources i-0123456789abcdef0\n",
		wantProblems: []string{fmt.Sprintf("line 1 names the CLI %q", "aws")},
	}, {
		name:         "a write outside a comment",
		text:         "{\"cmd\": \"bash -c 'aws ec2 create-tags --resources i-0123456789abcdef0'\"}\n",
		wantProblems: []string{fmt.Sprintf("line 1 names the CLI %q", "aws")},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, problems := programFindings(program, tc.text)

			var gotProblems []string

			for _, want := range tc.wantProblems {
				for _, problem := range problems {
					if strings.Contains(problem, want) {
						gotProblems = append(gotProblems, want)

						break
					}
				}
			}

			if diff := cmp.Diff(tc.wantProblems, gotProblems); diff != "" {
				t.Errorf("the reading of the program %q reports problems other than the ones it must (-want +got):\n%s\nproblems: %v",
					tc.text, diff, problems)
			}

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(cloudCommand{})); diff != "" {
				t.Errorf("the invocations of the program %q differ from the ones it holds (-want +got):\n%s", tc.text, diff)
			}

			var writes []string

			for _, w := range notReadOnly(got) {
				writes = append(writes, w.path)
			}

			if diff := cmp.Diff(tc.wantWrites, writes); diff != "" {
				t.Errorf("the read-only check over the program %q refuses %v, want %v: a program the guide ships may only read either",
					tc.text, writes, tc.wantWrites)
			}
		})
	}
}

// notReadOnly holds every invocation to its CLI's read commands and returns the
// ones that are no read. An invocation of a CLI the check does not know is one of
// them: it cannot say it reads.
func notReadOnly(commands []cloudCommand) []write {
	var writes []write

	for _, command := range commands {
		cli, known := cloudCLIByName(command.cli)
		if !known {
			writes = append(writes, write{where: command.where, cli: command.cli, command: command.String(), path: command.cli})

			continue
		}

		commandPath, read := cli.reads(command.words)
		if read {
			continue
		}

		writes = append(writes, write{
			where:   command.where,
			cli:     command.cli,
			command: command.String(),
			path:    commandPath,
		})
	}

	return writes
}

// subcommandPath returns the words of an invocation that name its command groups
// and its command, up to its first flag: `az servicebus queue list
// --namespace-name x` has the path servicebus, queue, list.
func subcommandPath(words []string) []string {
	for i, word := range words {
		if strings.HasPrefix(word, "-") {
			return words[:i]
		}
	}

	return words
}

// shippedProgram is one program in the guide's programs directory and the cloud
// CLI invocations its comments name, with the program's path relative to the
// repository root.
type shippedProgram struct {
	path     string
	commands []cloudCommand
}

// shippedPrograms returns the programs the guide ships, each read from its text. It
// fails the test for a file beside them that is no `.jq` program: a script added
// there — a shell helper, for example — holds commands this check must read, so it
// has to be read here on purpose rather than skipped.
func shippedPrograms(t *testing.T) []shippedProgram {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(theRepositoryRoot(t), filepath.FromSlash(guideProgramsDirectory), "*"))
	if err != nil {
		t.Fatalf("glob %s: %v", guideProgramsDirectory, err)
	}

	var programs []shippedProgram

	for _, match := range matches {
		name := filepath.Base(match)

		if filepath.Ext(name) != ".jq" {
			t.Errorf("%s holds %s, which is no .jq program: a script beside the programs holds cloud CLI commands this check does not read, so extend shippedPrograms to read it", guideProgramsDirectory, name)

			continue
		}

		programPath := guideProgramsDirectory + name
		commands, problems := programFindings(programPath, string(readRepositoryFile(t, programPath)))

		for _, problem := range problems {
			t.Error(problem)
		}

		programs = append(programs, shippedProgram{path: programPath, commands: commands})
	}

	return programs
}

// programFindings returns the cloud CLI invocations a program the guide ships
// names in its comments, and the problems of the backstop over every line of it: a
// name the comments' reading cannot place fails the check wherever the program
// writes it.
func programFindings(programPath, text string) ([]cloudCommand, []string) {
	lines := strings.Split(text, "\n")
	commands := scriptInvocations(programPath, text)

	return commands, unplacedCLINames(programPath, lines, commands)
}

// guideTextFindings returns the cloud CLI invocations a guide text holds — in its
// shell blocks, in the inline code spans of its prose and in the prose lines that
// start with a CLI name — and the problems that keep the check from reading it: a
// fence label that is neither a shell language nor on the closed list of prose
// languages, a fence that never closes, a shell text it cannot read to its end or
// that names its program through a variable, and every CLI name the backstop cannot
// place. A problem is a failure: a text the check could not read is no text it may
// pass.
//
// It walks the guide once rather than section by section, so it reads every block
// wherever it sits: under a heading that repeats, above the guide's first heading,
// and in a fence the guide labels by hand.
func guideTextFindings(guide string) ([]cloudCommand, []string) {
	var (
		commands []cloudCommand
		problems []string
		section  string
	)

	lines := strings.Split(guide, "\n")

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		// Only a `#` line outside a fence is a heading: the command blocks hold
		// comments too, and a comment is no section of the guide.
		if heading := headingText(trimmed); heading != "" {
			section = heading

			continue
		}

		// open is the line the fence opens on: a fence problem names it, not the
		// line the body ends on, so the reader finds the block.
		if marker, info, opens := fenceOpen(lines[i]); opens {
			open := i + 1

			body, last, closed := fenceBody(lines, i, marker)
			if !closed {
				problems = append(problems, fmt.Sprintf("%s, the fence that opens at line %d: it never closes, so the check cannot see where what it holds ends",
					guideLocation(section), open))
			}

			i = last

			shell, err := fenceLanguage(leadingWord(info))

			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("%s, the fence that opens at line %d: %v", guideLocation(section), open, err))
			case shell:
				// The body's first line is the file's line open+1, and
				// invocationsIn counts from the text's first line.
				found, refused := invocationsIn(strings.Join(body, "\n"), true, guideLocation(section), open+1)
				commands = append(commands, found...)
				problems = append(problems, refused...)
			}

			continue
		}

		where := guideLocation(section)

		for _, span := range codeSpans(lines[i]) {
			found, refused := invocationsIn(span, true, where, i+1)
			commands = append(commands, found...)
			problems = append(problems, refused...)
		}

		if startsWithCloudCLI(trimmed) {
			found, refused := invocationsIn(trimmed, true, where, i+1)
			commands = append(commands, found...)
			problems = append(problems, refused...)
		}
	}

	return commands, append(problems, unplacedCLINames(inventoryGuidePath, lines, commands)...)
}

// guideShellLanguages are the languages of a fence the check reads for commands:
// the shell languages the guide's command blocks are written in, and no label at
// all, since a fence of bare commands is bash.
var guideShellLanguages = []string{"bash", "console", "sh", "shell", "zsh"}

// guideProseLanguages are the languages of a fence that holds no command, a closed
// list: the JSON samples the guide prints and its plain text. A fence with any
// other language fails the check, so a block it does not read is a deliberate
// addition to this list rather than a block that slipped past.
var guideProseLanguages = []string{"json", "text"}

// guideBareMentions are the words today's guide and programs write after a cloud
// CLI's name when they name the CLI and run no command. A bare mention is narrow on
// purpose: a CLI name followed by any other word fails the check rather than passing
// as prose, so a command the extraction could not read cannot hide behind one. Each
// word is here for a line of today's text:
//
//   - ".", the full stop after the directory the guide empties: `rm -r inventories/aws`.
//   - "CLI", aws.jq's first line: "the read-only aws CLI calls".
//   - "a", the guide's note on the projections: "`--query` for `aws` and `az`, a `--format`".
//   - "and", the same sentence: "for `aws` and `az`".
//   - "call", its note on the quiet flag: "Every `gcloud` call above carries `--quiet`".
//   - "calls", azure.jq's and gcp.jq's first lines: "the read-only az calls".
//   - "gcp", the guide's source table: "`aws`, `gcp` or `azure` here".
//   - "or", the guide's requirements: "the provider's CLI (`aws` version 2, `gcloud`, or `az` …)".
//   - "so", the note on the projections: "for `gcloud`), so it runs once".
//   - "version", the requirements line: "`aws` version 2".
//   - "with", the requirements line: "`az` with the `resource-graph` extension".
//   - "writes", gcp.jq's last comment: "as gcloud writes a string".
var guideBareMentions = []string{
	".", "CLI", "a", "and", "call", "calls", "gcp", "or", "so", "version", "with", "writes",
}

// unplacedCLINames returns a problem for every line of a text that names a cloud
// CLI where the check can neither see an invocation the extraction found nor a bare
// mention of the CLI: the fail-closed backstop of the whole check (issue #716).
//
// It trusts no Markdown and no shell reading at all. It splits every line — inside a
// fence of any language, in prose, in a heading, in a blockquote, in an indented
// block, in HTML, in a heredoc body — into fields on white space and shell
// punctuation, takes each field's last path element, and holds every field that is
// one of the three names to account, in the line's order: a bare mention, then one of
// the line's invocation starts (each start once), then a field sequence that reads
// as one of the CLI's read commands — a read named in text the extraction does not
// parse, as azure.jq's prose writes "from az vm list-sizes", breaks no rule.
// Anything else is a command the check cannot read, and that fails.
func unplacedCLINames(file string, lines []string, commands []cloudCommand) []string {
	starts := make(map[int]int, len(commands))

	for _, command := range commands {
		starts[command.line]++
	}

	var problems []string

	for at, line := range lines {
		number := at + 1
		budget := starts[number]
		fields := lineFields(line)

		for i, field := range fields {
			name := cliNameField(field)
			if name == "" {
				continue
			}

			if i+1 == len(fields) || slices.Contains(guideBareMentions, path.Base(fields[i+1])) {
				continue
			}

			if budget > 0 {
				budget--

				continue
			}

			if cli, known := cloudCLIByName(name); known {
				if _, read := cli.reads(fields[i+1:]); read {
					continue
				}
			}

			problems = append(problems, fmt.Sprintf("%s: line %d names the CLI %q, and the check cannot place it: no invocation the extraction finds starts on that line, the word after it is no bare mention guideBareMentions lists, and the words after it read nothing, so it could be a command the extraction cannot read — a `bash -c` argument, a heredoc body, an indented block, an HTML block, a code span over two lines, a fence it skipped (CLAUDE.md rule 1, the guide may only read)",
				file, number, name))
		}
	}

	return problems
}

// lineFieldSeparators are the characters unplacedCLINames splits a line into fields
// on: white space, the double quote, the backtick, `$`, the shell's braces,
// parentheses and operators, `=` and `,`, and the backslash. A single quote is no
// separator: the guide's programs write a possessive through one — gcp.jq's
// "gcloud's" — so it splits no word, and cliNameField strips the quote a shell text
// puts before a command's name.
const lineFieldSeparators = " \t\r\f\v`\"$(){};|&<>=,\\"

// lineFields returns the fields of a line, in the line's order.
func lineFields(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return strings.ContainsRune(lineFieldSeparators, r)
	})
}

// cliNameField returns the cloud CLI's name a field names, and "" for a field that
// names none: the field without the single quote around it, its last path element,
// so `/usr/local/bin/aws` and `bash -c 'aws …'` name aws, and no name for a field
// that ends in a path separator, which names a directory — the guide's
// `inventories/aws/` is no invocation.
func cliNameField(field string) string {
	field = strings.Trim(field, "'")
	if field == "" || strings.HasSuffix(field, "/") {
		return ""
	}

	name := path.Base(field)
	if !isCloudCLIName(name) {
		return ""
	}

	return name
}

// fenceLanguage returns whether a fence's language is a shell one, and an error for
// a language that is neither a shell one nor on the closed list of prose languages.
func fenceLanguage(label string) (bool, error) {
	if label == "" || slices.Contains(guideShellLanguages, label) {
		return true, nil
	}

	if slices.Contains(guideProseLanguages, label) {
		return false, nil
	}

	return false, fmt.Errorf("a fence is labelled %q, which is no shell language (%s) and on no closed list of prose languages (%s), so the check reads no command in it: label it as the shell text it is, or add its language to guideProseLanguages on purpose",
		label, strings.Join(guideShellLanguages, ", "), strings.Join(guideProseLanguages, ", "))
}

// guideLocation names where in the guide a text sits: its section, or the part
// above the guide's first heading.
func guideLocation(section string) string {
	if section == "" {
		return inventoryGuidePath + ", before the first heading"
	}

	return fmt.Sprintf("%s, section %q", inventoryGuidePath, section)
}

// leadingWord returns the first word of a text, "" for a text with no word.
func leadingWord(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

// fenceOpen reports whether a line opens a fenced block — CommonMark's opener: a run
// of at least three backticks or tildes, indented by at most three spaces, so an
// indented code block whose text happens to be ``` opens none — and returns the
// fence's marker, the character it is made of repeated its length, and its info
// string, so a `~~~bash title="x"` fence is as much a fence as a ```bash one. A
// backtick fence whose info string holds a backtick is no fence either, as
// CommonMark says: it opens nothing, so the block after it is read on its own.
func fenceOpen(line string) (marker, info string, ok bool) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 {
		return "", "", false
	}

	line = strings.TrimRight(line[indent:], " \t\r")

	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return "", "", false
	}

	length := 0

	for length < len(line) && line[length] == line[0] {
		length++
	}

	if length < 3 {
		return "", "", false
	}

	info = strings.TrimSpace(line[length:])
	if line[0] == '`' && strings.ContainsRune(info, '`') {
		return "", "", false
	}

	return strings.Repeat(line[:1], length), info, true
}

// fenceClose reports whether a trimmed line closes a fence with the marker: the
// marker's character alone, at least as many times as it opens with.
func fenceClose(line, marker string) bool {
	if len(line) < len(marker) {
		return false
	}

	for i := 0; i < len(line); i++ {
		if line[i] != marker[0] {
			return false
		}
	}

	return true
}

// fenceBody returns the lines of the fenced block that opens at the line index, the
// index of the line that closes it, and whether one does.
func fenceBody(lines []string, open int, marker string) (body []string, last int, closed bool) {
	for i := open + 1; i < len(lines); i++ {
		if fenceClose(strings.TrimSpace(lines[i]), marker) {
			return body, i, true
		}

		body = append(body, lines[i])
	}

	return body, len(lines) - 1, false
}

// codeSpans returns the text of the inline code spans of a Markdown line: the text
// between pairs of backticks, in the line's order. A backtick with no pair is no
// span, so a line that happens to hold one is read as the prose it is.
func codeSpans(line string) []string {
	var (
		spans []string
		runes = []rune(line)
		open  = -1
	)

	for i, r := range runes {
		if r != '`' {
			continue
		}

		if open < 0 {
			open = i

			continue
		}

		spans = append(spans, string(runes[open+1:i]))
		open = -1
	}

	return spans
}

// startsWithCloudCLI reports whether a line's first word is a CLI name.
func startsWithCloudCLI(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}

	_, ok := cloudCLIName(shellToken{text: fields[0]})

	return ok
}

// invocationsIn returns every cloud CLI invocation of a shell text, and where it
// found them: from the token that names a CLI to the operator or the CLI that
// ends it, so an invocation after a pipe, inside `$(...)` or a backtick, after
// `&&` or after xargs is found like one that starts a line, and a flag's quoted
// argument is one word however many lines it spans. firstLine is the line of the
// file the text's first line is, so each invocation names the line its CLI's name
// is on.
//
// It reports the texts the check may not pass: one it cannot read to its end — an
// unbalanced quote or substitution, a heredoc with no delimiter line — and one that
// names its program through a variable, as `cli=aws; "$cli" ec2 create-tags` does,
// which runs for the visitor as surely as a command of its own.
func invocationsIn(text string, newlineEndsCommand bool, where string, firstLine int) ([]cloudCommand, []string) {
	tokens, err := shellTokens(text, newlineEndsCommand)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", where, err)}
	}

	var (
		commands []cloudCommand
		problems []string
		// commandPosition is where a program's name stands: at the text's start and
		// after every operator. A redirect's target is a file, so it is no program
		// position, and neither is a word behind one.
		commandPosition = true
		redirectTarget  = false
	)

	for i, token := range tokens {
		if token.operator {
			commandPosition = token.text != "<" && token.text != ">"
			// A redirect's next word is the file it reads or writes, not a
			// program: a `<<` heredoc's delimiter is no token, so the word
			// after its operator is a command again.
			redirectTarget = !commandPosition

			continue
		}

		if redirectTarget {
			redirectTarget = false

			continue
		}

		cli, invocation := cloudCLIName(token)
		if !invocation {
			if commandPosition && strings.HasPrefix(token.text, "$") {
				problems = append(problems, fmt.Sprintf("%s, line %d: %q stands where a program's name goes and begins with $, so a variable could name any program — these three among them: the check reads the guide's own words, not the visitor's environment (CLAUDE.md rule 1, the guide may only read)",
					where, firstLine+token.line-1, token.text))
			}

			commandPosition = false

			continue
		}

		var words []string

		for _, next := range tokens[i+1:] {
			if next.operator {
				break
			}

			if _, another := cloudCLIName(next); another {
				break
			}

			words = append(words, next.text)
		}

		if len(words) == 0 {
			// A bare mention of a CLI, as the guide's prose writes `aws` alone,
			// runs no command.
			commandPosition = false

			continue
		}

		commands = append(commands, cloudCommand{
			cli:   cli,
			words: words,
			where: where,
			line:  firstLine + token.line - 1,
		})
		commandPosition = false
	}

	return commands, problems
}

// scriptInvocations returns the cloud CLI invocations a program the guide ships
// names in its comments: a comment line that holds a CLI name anywhere starts one —
// `# then run: aws ec2 create-tags` is such a line — and the following comment
// lines that read as a subcommand continuation (a subcommand group, a flag) go on
// with it, so a command the program wraps across two lines is read whole. A CLI
// name that follows a word, as `as gcloud writes a string` writes it, is the
// comment's prose about the CLI and starts none.
func scriptInvocations(programPath, text string) []cloudCommand {
	var (
		commands []cloudCommand
		open     *cloudCommand
	)

	closeOpen := func() {
		if open != nil {
			commands = append(commands, *open)
			open = nil
		}
	}

	for number, line := range strings.Split(text, "\n") {
		content, isComment := strings.CutPrefix(strings.TrimSpace(line), "#")
		if !isComment {
			closeOpen()

			continue
		}

		content = strings.TrimSpace(content)

		fields := strings.Fields(content)
		start := commandStart(content)

		switch {
		case len(fields) == 0:
			closeOpen()
		case start >= 0:
			closeOpen()

			// A comment wraps a command in prose, and a comma ends it there:
			// no CLI command path holds one.
			command := content[start:]
			if comma := strings.IndexByte(command, ','); comma >= 0 {
				command = command[:comma]
			}

			name := path.Base(leadingWord(command))

			open = &cloudCommand{
				cli:   name,
				words: wordTokens(strings.TrimSpace(strings.TrimPrefix(command, leadingWord(command)))),
				where: fmt.Sprintf("%s, line %d", programPath, number+1),
				line:  number + 1,
			}
		case open != nil && continuesSubcommand(content):
			open.words = append(open.words, wordTokens(content)...)
		default:
			closeOpen()
		}
	}

	closeOpen()

	return commands
}

// commandStart returns where in a comment's text the command it names starts, or -1
// for a text that names none: the first word that is a CLI name and that begins a
// command — at the start of the comment, or after a character that no word ends,
// such as `(`, `}` or `:`. A CLI name that follows a word is the comment's prose
// about the CLI and starts no command, so `the read-only aws CLI calls` and `as
// gcloud writes a string` name none.
func commandStart(content string) int {
	for at := 0; at < len(content); at++ {
		// A word starts after white space or after a character no word is made
		// of, such as the `(` and `}` a comment puts beside a command.
		if at > 0 && isWordByte(content[at-1]) {
			continue
		}

		if content[at] == ' ' || content[at] == '\t' {
			continue
		}

		end := at
		for end < len(content) && content[end] != ' ' && content[end] != '\t' {
			end++
		}

		if _, ok := cloudCLIName(shellToken{text: content[at:end]}); !ok {
			continue
		}

		before := strings.TrimRight(content[:at], " \t")
		if before != "" && isWordByte(before[len(before)-1]) {
			continue
		}

		return at
	}

	return -1
}

// continuesSubcommand reports whether a comment line goes on with the invocation
// above it: it starts with a lower-case word, as `subscription list` and
// `blob-service-properties show` do, or with a flag. A line that starts with
// anything else, such as the JSON shape a program's comment puts beside a
// command, is its own text and ends the invocation.
func continuesSubcommand(content string) bool {
	if content == "" {
		return false
	}

	first := content[0]

	return first == '-' || (first >= 'a' && first <= 'z')
}

// wordTokens returns the words of a text, its operators left out. A comment line is
// prose, so a quote it leaves open is no problem to report here.
func wordTokens(text string) []string {
	tokens, _ := shellTokens(text, false)

	var words []string

	for _, token := range tokens {
		if !token.operator {
			words = append(words, token.text)
		}
	}

	return words
}

// cloudCLIName returns the CLI the token names, and whether it names one: its last
// path element, so `/usr/bin/aws` is aws and `aws.jq` is no invocation.
func cloudCLIName(token shellToken) (string, bool) {
	if token.operator {
		return "", false
	}

	name := cliNameField(token.text)
	if name == "" {
		return "", false
	}

	return name, true
}

// isCloudCLIName reports whether the word is one of the three cloud CLIs' names.
func isCloudCLIName(word string) bool {
	return slices.ContainsFunc(cloudCLIs, func(cli cloudCLI) bool { return cli.name == word })
}

// isWordByte reports whether the byte can be part of a shell word: a letter, a
// digit, or one of the characters a program's name and arguments are made of.
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' || b == '/'
}
