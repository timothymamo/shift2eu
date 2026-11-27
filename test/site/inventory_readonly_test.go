package site_test

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file holds the guide-wide read-only check of the inventory guide (issue
// #716): every `aws`, `az` and `gcloud` command the guide tells a visitor to run
// must only read. CLAUDE.md rule 1 forbids writing to a cloud, and the guide's
// commands run against the visitor's own account with the visitor's own
// credentials, so a `create`, `delete` or `set` no check catches would be a write
// this project tells its readers to make.
//
// One mechanism decides it, and it trusts no Markdown and no shell reading: a field
// scan of every line of the guide and of every program it ships. The scan joins the
// lines a trailing `\` continues, splits every other line into fields on white
// space and shell punctuation, takes each field's last path element, and holds every
// field that is one of the three names to account. A name is either a bare mention
// (the next field is one of guideBareMentions' words), or it starts a field sequence
// that is a read: one of the exact command paths cloudCLIs lists for the CLI, with
// no denied flag, and `az rest` as one GET of the management endpoint. Every other
// field sequence fails the check, wherever it sits: in a shell fence, in a ```text
// or ```json sample, in prose, in a heading, in a blockquote, in an indented block,
// in an HTML block, in a code span over two lines, or in a heredoc body.
//
// A line that ends at its last field is continued by the next line's fields, so a
// command the text wraps — after the CLI's name in a code span, a prose line, a
// fence body or a heredoc body, or in the middle of a program's comment table — is
// read whole. The guard is the line's last character: the guide's JSON writes
// `"source": "aws",`, whose comma ends the mention, not the `"resources"` array that
// follows it, while a name that ends its line does take the next line's fields.
//
// An invocation's command path is held to the exact list of read commands its CLI
// has in cloudCLIs, not to a verb: a new command is a deliberate edit of that list.
// The words after the command path are the command's positional arguments, so
// `gcloud compute instances delete list` reads nothing and fails, and a read that
// returns a secret (`aws secretsmanager get-secret-value`) is no read the guide may
// run. A flag that points the CLI elsewhere, feeds it input of the caller's
// choosing or turns its checks off (`--endpoint-url`, `--no-verify-ssl`,
// `--ca-bundle`, `--cli-input-json`, `--impersonate-service-account`,
// `--flags-file`) fails even on a path that reads, and so does the flag's prefix,
// which the CLIs' parsers complete (`--endpoint`, `--impersonate`). A shell
// variable the CLIs read in a denied flag's place (`AWS_ENDPOINT_URL`,
// `AWS_CA_BUNDLE`, `CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT`) fails on any line
// that names the CLI. `az rest` reads only as one GET of the management endpoint,
// with no resource, headers or body, and with its method flag spelt out.
//
// A global flag before the command path (`aws --output json ec2 describe-instances`)
// is no read on purpose: the command path is the words up to the first flag, so the
// check fails closed rather than parse a path past a flag a future denied one could
// hide behind.
//
// Residuals, the escapes this check knowingly leaves, named here in one place:
//
//   - A name the field scan never sees as one field: `a""ws`, `\a\w\s` and
//     `$'\x61ws'` spell a CLI's name in bytes no field equals.
//   - A command run by something the guide does not ship: a downloaded script, an
//     alias an earlier command defined, a `make` target whose recipe lives in a
//     Makefile. The check reads the guide's text and its programs.
//   - A CLI's name a variable holds: `cli=aws` on one line and `"$cli" ec2
//     create-tags` on the next names no field the scan sees as a CLI. The same line
//     is caught, since the assignment's value is a field of it.
//   - An `az rest` that gives no address: the guide's prose names `az rest
//     --method get --url` with no value, so a `--url` whose next word is no address
//     (it starts with no scheme or `$`, and holds no `.` or `/`) and is followed by
//     more words counts as no address given, and the one method, get, is what
//     decides.
//   - A flag written after the closing quote of an argument that spans physical
//     lines without a trailing `\`: the scan reads each logical line, so the flag
//     belongs to no name's field sequence and a denied one there is not seen.
//   - A redirect target: `> "$out/x.json"` holds no CLI's name, and a target that
//     named one is read as a field like any other, not as the file shell writes.
//   - CommonMark's indentation and info strings, read by the fence label guard: a
//     fence opens with at most three spaces of indent and, when it is a backtick
//     fence, with no backtick in its info string. The scan reads every line of every
//     fence whatever its label, so the guard only makes a new language deliberate.
//   - The bare mentions are the words today's text uses after a name: a guide that
//     writes the CLI and then prose fails the check until its word is listed, which
//     is the point — an unlisted word could be a command's first word.
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
// the exact command paths its invocations may name, the flags and variables no read
// may use, and the command reference those paths come from, the source of every
// read command the list holds (issue #716's review: a verb rule let a write whose
// last word was a read verb through).
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
	// --endpoint-url sends the request to a host of the caller's choosing,
	// --no-verify-ssl and --ca-bundle turn off the check that the answer comes from
	// AWS, and --cli-input-json and --cli-input-yaml read the call's parameters from
	// a file: a read that passes any of them reads something else's account. The
	// variables are what the CLI reads in their place.
	deniedFlags: []string{"--ca-bundle", "--cli-input-json", "--cli-input-yaml", "--endpoint-url", "--no-verify-ssl"},
	deniedEnv: []deniedEnv{
		{variable: "AWS_CA_BUNDLE", flag: "--ca-bundle"},
		{variable: "AWS_ENDPOINT_URL", flag: "--endpoint-url"},
	},
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
	// visitor may not own a read of, --flags-file reads the flags from a file the
	// check does not read, --access-token-file takes the token from a file, and
	// --configuration runs another named configuration's credentials.
	deniedFlags: []string{"--access-token-file", "--configuration", "--flags-file", "--impersonate-service-account"},
	deniedEnv: []deniedEnv{
		{variable: "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", flag: "--impersonate-service-account"},
	},
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
	// command path: a read that points the CLI elsewhere, feeds it input of the
	// caller's choosing or turns its checks off is no read the guide may run. A
	// flag's prefix is one of them too, since the CLI's parser completes it.
	deniedFlags []string
	// deniedEnv are the environment variables the CLI reads in a denied flag's
	// place: a line that sets one and names the CLI is no read either.
	deniedEnv []deniedEnv
}

// deniedEnv is an environment variable that does what a denied flag does: the CLI
// reads it in the flag's place.
type deniedEnv struct {
	// variable is the variable's name; one of its per-service spellings, such as
	// AWS_ENDPOINT_URL_S3, starts with it and an underscore.
	variable string
	// flag is the flag the variable stands in for.
	flag string
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

// reads returns the command path a field sequence names, as the guide writes it,
// and whether the sequence is a read: the path must start with one of the CLI's read
// commands, no word may name a flag the CLI denies, and `az rest`, the one command
// whose read depends on flags, must be one GET of the management endpoint. The path
// it returns holds the word that made the sequence no read, so the message says
// where the check stopped.
func (c cloudCLI) reads(words []string) (string, bool) {
	commandPath := subcommandPath(words)
	held := strings.Join(commandPath, " ")

	for _, word := range words {
		if c.denies(flagName(word)) {
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

// denies reports whether a flag's name is one of the CLI's denied flags, in full or
// as a truncation of one that the CLI's parser completes: --endpoint for
// --endpoint-url, --head for --headers. Nothing shorter than the two dashes and a
// letter is a truncation.
func (c cloudCLI) denies(flag string) bool {
	for _, denied := range c.deniedFlags {
		if flag == denied {
			return true
		}

		if strings.HasPrefix(flag, "--") && len(flag) > 2 && strings.HasPrefix(denied, flag) {
			return true
		}
	}

	return false
}

// environmentOverride returns the assignment on a line that sets one of the CLI's
// denied variables, the flag the variable stands in for, and whether there is one.
// A variable's per-service spelling (`AWS_ENDPOINT_URL_S3`) sets it too.
func (c cloudCLI) environmentOverride(line string) (assignment, flag string, found bool) {
	for _, word := range strings.Fields(line) {
		name, _, assigned := strings.Cut(strings.TrimLeft(word, "'\"`$()[]{};&|<>\\"), "=")
		if !assigned {
			continue
		}

		for _, denied := range c.deniedEnv {
			if name == denied.variable || strings.HasPrefix(name, denied.variable+"_") {
				return word, denied.flag, true
			}
		}
	}

	return "", "", false
}

// azRestReads returns what an `az rest` sequence is held to, and whether it reads:
// exactly one method, get, and, when it gives an address, one on the management
// endpoint. An invocation that abbreviates the method flag — `-mpost`, `-m=post`,
// `--meth put` — is refused rather than read, since the check would otherwise take
// it for one with no method at all and let the write through.
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
			switch {
			case inline:
				address, urlFlag = value, name
			case i+1 == len(words):
				// No value at all: the guide's prose ends a code span at `--url`.
				address, urlFlag = "", name
			case looksLikeAddress(words[i+1]) || i+2 == len(words):
				// The value is the next word: an address, or a word the check
				// cannot tell from prose that ends the sequence, which the
				// endpoint check then refuses unless it is the management
				// endpoint's.
				i++

				address, urlFlag = words[i], name
			default:
				// The next word is prose and the sequence goes on after it: the
				// guide's sentence continues after a code span that ended at
				// `--url`, and no address is given. A write that gives none is
				// held to its method alone.
				address, urlFlag = "", name
			}
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

// looksLikeAddress reports whether a word can be the address `az rest --url` is
// given: a scheme, a `$` substitution or a path, rather than the prose the guide's
// sentence puts after a code span that ends at `--url`.
func looksLikeAddress(word string) bool {
	return strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://") ||
		strings.HasPrefix(word, "$") || strings.HasPrefix(word, "/") ||
		strings.ContainsAny(word, "./")
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

// TestInventoryGuideCommandsAreReadOnly holds every `aws`, `az` and `gcloud`
// command of the inventory guide, and of the programs it ships, to its CLI's read
// commands (issue #716, CLAUDE.md rule 1). The floors below are what keeps the
// check from passing over a scan that found nothing: each CLI's commands are in the
// guide today, and a splitter that quietly lost them fails here.
func TestInventoryGuideCommandsAreReadOnly(t *testing.T) {
	t.Parallel()

	guide := scanText(inventoryGuidePath, string(readRepositoryFile(t, inventoryGuidePath)))
	for _, problem := range guide.problems {
		t.Error(problem)
	}

	programs := shippedPrograms(t)

	// The guide's text holds 36 aws, 21 az and 25 gcloud field sequences that read
	// today, and the programs' comments add 13 az (azure.jq's header table and its
	// prose) and 1 gcloud (gcp.jq's first paragraph); aws.jq's comments name the
	// operations without the CLI's name, so they add none. The floors are below those
	// counts, so the guide may lose a command without failing here, while a splitter
	// that broke fails at once.
	guideFloors := []struct {
		cli     string
		atLeast int
	}{
		{cli: "aws", atLeast: 33},
		{cli: "az", atLeast: 19},
		{cli: "gcloud", atLeast: 23},
	}

	for _, floor := range guideFloors {
		if guide.accepted[floor.cli] < floor.atLeast {
			t.Errorf("%s holds %d %s reads, want at least %d, so this check read something: the field scan reads every line of the guide, and a splitter that finds nothing would pass every check over an empty list",
				inventoryGuidePath, guide.accepted[floor.cli], floor.cli, floor.atLeast)
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
		{program: guideProgramsDirectory + "azure.jq", atLeast: 11},
		{program: guideProgramsDirectory + "gcp.jq", atLeast: 1},
	}

	for _, floor := range programFloors {
		found := -1

		for _, program := range programs {
			if program.path == floor.program {
				found = totalReads(program.scan.accepted)
			}
		}

		if found < 0 {
			t.Errorf("the programs hold no %s, want it read here: a program the guide ships names commands too", floor.program)

			continue
		}

		if found < floor.atLeast {
			t.Errorf("%s holds %d reads, want at least %d: a program the guide ships names commands too, and a scan that stopped reading them would pass every check over an empty list",
				floor.program, found, floor.atLeast)
		}
	}
}

// totalReads returns how many read sequences a field scan accepted, all CLIs
// together: the count a program's floor holds.
func totalReads(accepted map[string]int) int {
	total := 0

	for _, found := range accepted {
		total += found
	}

	return total
}

// TestReadOnlyCheckFlagsPlantedWrites shows the check bites (issue #716): a write
// planted into a copy of the guide, in memory, is reported and the rest of the
// guide is not. Each case plants its command in a shape the text can take — a
// fence of a shell language, a ```text or ```json sample, an inline code span, a
// prose line, a blockquote, an indented block, an HTML block, a heredoc body, a
// line the text wraps after the CLI's name, a variable that names a denied flag —
// and names the problem the field scan must report for it: the line, and the
// command it read there. The case of a variable that holds the CLI's name plants
// what the scan does see of that residual — the assignment's value on the same line
// — while a variable set on another line stays the escape the header names.
func TestReadOnlyCheckFlagsPlantedWrites(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, inventoryGuidePath))

	const awsWrite = "aws ec2 create-tags --resources i-0123456789abcdef0 --tags Key=Name,Value=shop-web"

	fence := func(open, close, command string) string {
		return guide + "\n\n" + open + "\n" + command + "\n" + close + "\n"
	}

	// plantedLine is the line the text appended below starts on, and appended
	// returns the guide with lines of text of its own after it.
	plantedLine := len(strings.Split(guide, "\n")) + 2

	appended := func(lines ...string) string {
		return guide + "\n\n" + strings.Join(lines, "\n") + "\n"
	}

	// at names a problem by the file line it reports and a substring it must hold:
	// the command the scan read there.
	at := func(line int, holds string) string {
		return fmt.Sprintf(":%d: %s", line, holds)
	}

	tests := []struct {
		name string
		// mutated is the guide with the case's plant.
		mutated string
		// want are the substrings of the problems the scan must report, one per
		// problem.
		want []string
	}{{
		name:    "a planted aws write",
		mutated: fence("```bash", "```", awsWrite),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a planted az write",
		mutated: fence("```bash", "```", "az group delete --name shop-web --yes"),
		want:    []string{at(plantedLine+1, "az group delete")},
	}, {
		name:    "a planted gcloud write",
		mutated: fence("```bash", "```", "gcloud compute instances delete shop-web --zone=europe-west1-b --quiet"),
		want:    []string{at(plantedLine+1, "gcloud compute instances delete")},
	}, {
		name:    "a planted az rest that is no GET",
		mutated: fence("```bash", "```", "az rest --method post --url https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/shop-web"),
		want:    []string{at(plantedLine+1, "az rest --method")},
	}, {
		name:    "a planted az rest that passes two methods",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --method post"),
		want:    []string{at(plantedLine+1, "az rest --method")},
	}, {
		name:    "a write under a duplicate heading",
		mutated: guide + "\n\n## Validate the result\n\n```bash\n" + awsWrite + "\n```\n",
		want:    []string{at(plantedLine+3, "aws ec2 create-tags")},
	}, {
		name:    "a write before the first heading",
		mutated: "```bash\n" + awsWrite + "\n```\n\n" + guide,
		want:    []string{at(2, "aws ec2 create-tags")},
	}, {
		name:    "a write in a sh fence",
		mutated: fence("```sh", "```", awsWrite),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a write in a tilde fence",
		mutated: fence("~~~bash", "~~~", awsWrite),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a write in an inline code span",
		mutated: appended("Tag the instance by hand: `" + awsWrite + "`."),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a prose line",
		mutated: appended(awsWrite),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a gcloud positional argument that ends in a read word",
		mutated: fence("```bash", "```", "gcloud compute instances delete list"),
		want:    []string{at(plantedLine+1, "gcloud compute instances delete list")},
	}, {
		name:    "an az positional argument that ends in a read word",
		mutated: fence("```bash", "```", "az vm delete show --name shop-web"),
		want:    []string{at(plantedLine+1, "az vm delete show")},
	}, {
		name:    "an aws read that returns a secret",
		mutated: fence("```bash", "```", "aws secretsmanager get-secret-value --secret-id shop-db"),
		want:    []string{at(plantedLine+1, "aws secretsmanager get-secret-value")},
	}, {
		// A CLI's name wherever it stands: a quoted argument, a heredoc body, an
		// indented or blockquoted line, an HTML block, a code span over two lines,
		// a sample in a fence of any language.
		name:    "a write in a bash -c argument",
		mutated: appended(`bash -c 'aws --region eu-west-1 ec2 create-tags --resources i-0123456789abcdef0'`),
		want:    []string{at(plantedLine, "aws --region eu-west-1 ec2 create-tags")},
	}, {
		name:    "a write in a bash -c argument a path names",
		mutated: appended(`bash -c '/usr/local/bin/aws ec2 create-tags --resources i-0123456789abcdef0'`),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a bash -c argument after two spaces",
		mutated: appended(`bash -c 'aws  ec2 create-tags --resources i-0123456789abcdef0'`),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a bash -c argument after a tab",
		mutated: appended("bash -c 'aws\tec2 create-tags --resources i-0123456789abcdef0'"),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a bash -c argument that quotes its command group",
		mutated: appended(`bash -c 'aws "ec2" create-tags --resources i-0123456789abcdef0'`),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in the body of a bash heredoc",
		mutated: appended("```bash", "bash <<'EOF'", awsWrite, "EOF", "```"),
		want:    []string{at(plantedLine+2, "aws ec2 create-tags")},
	}, {
		name:    "a write in the body of a heredoc piped to sh",
		mutated: appended("```bash", "cat <<EOF | sh", awsWrite, "EOF", "```"),
		want:    []string{at(plantedLine+2, "aws ec2 create-tags")},
	}, {
		name:    "a write in a blockquoted fence",
		mutated: appended("> ```bash", "> "+awsWrite, "> ```"),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a write in an indented block behind sudo",
		mutated: appended("    sudo " + awsWrite),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in an indented block behind an environment assignment",
		mutated: appended("    AWS_PROFILE=shop " + awsWrite),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in an HTML block",
		mutated: appended("<pre><code>" + awsWrite + "</code></pre>"),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a text fence",
		mutated: appended("```text", awsWrite, "```"),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a write in a json fence",
		mutated: appended("```json", awsWrite, "```"),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a write in a code span of two backticks",
		mutated: appended("Tag it by hand: ``" + awsWrite + "``."),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a code span that spans two lines",
		mutated: appended("Tag it by hand: `aws ec2 create-tags --resources", "  i-0123456789abcdef0`."),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in a code span of a heading",
		mutated: appended("## Tag it with `" + awsWrite + "`"),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		// CommonMark: a fence opens with at most three spaces of indent, and a
		// backtick fence's info string holds no backtick, so neither line opens a
		// fence that could swallow the block of commands after it.
		name:    "a fence indented four spaces swallows no write",
		mutated: appended("    ```text", "```bash", awsWrite, "```"),
		want:    []string{at(plantedLine+2, "aws ec2 create-tags")},
	}, {
		name:    "a fence whose info string holds a backtick swallows no write",
		mutated: appended("```text `x`", "```bash", awsWrite, "```"),
		want:    []string{at(plantedLine+2, "aws ec2 create-tags")},
	}, {
		// A command the text wraps after the CLI's name: the name is the line's
		// last field, and the words that make it a write are on the next line.
		name:    "a write a code span wraps after the CLI's name",
		mutated: appended("Tag it by hand with `aws", "ec2 create-tags --resources i-0123456789abcdef0`."),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write prose wraps after the CLI's name",
		mutated: appended("Tag it by hand: aws", "ec2 create-tags --resources i-0123456789abcdef0"),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write a text fence wraps after the CLI's name",
		mutated: appended("```text", `aws \`, " ec2 create-tags --resources i-0123456789abcdef0", "```"),
		want:    []string{at(plantedLine+1, "aws ec2 create-tags")},
	}, {
		name:    "a write a heredoc wraps after the CLI's name",
		mutated: appended("```bash", "cat <<EOF | sh", `aws \`, " ec2 create-tags --resources i-0123456789abcdef0", "EOF", "```"),
		want:    []string{at(plantedLine+2, "aws ec2 create-tags")},
	}, {
		// A read and a write on one line: the read's words pay for no write.
		name:    "a write before a read on one line",
		mutated: appended(`bash -c 'aws ec2 create-tags --resources i-0123456789abcdef0' && aws ec2 describe-instances`),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write in an eval argument",
		mutated: appended(`eval "aws ec2 create-tags --resources i-0123456789abcdef0"`),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		name:    "a write an xargs argument runs",
		mutated: appended(`xargs -r sh -c 'aws ec2 create-tags --resources i-0123456789abcdef0'`),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		// A shell variable names what a denied flag names: the read passes no such
		// flag, but the CLI reads the variable in its place.
		name:    "a read whose endpoint a variable names",
		mutated: appended("AWS_ENDPOINT_URL=https://example.com aws ec2 describe-instances"),
		want:    []string{at(plantedLine, "AWS_ENDPOINT_URL=https://example.com")},
	}, {
		name:    "a read whose certificate bundle a variable names",
		mutated: appended("AWS_CA_BUNDLE=./ca.pem aws ec2 describe-instances"),
		want:    []string{at(plantedLine, "AWS_CA_BUNDLE=./ca.pem")},
	}, {
		name:    "a read whose impersonation a variable names",
		mutated: appended("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT=sa@example-project.iam.gserviceaccount.com gcloud compute instances list"),
		want:    []string{at(plantedLine, "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT=sa@example-project.iam.gserviceaccount.com")},
	}, {
		name:    "a variable after the read it changes",
		mutated: appended("aws ec2 describe-instances AWS_ENDPOINT_URL=https://example.com"),
		want:    []string{at(plantedLine, "AWS_ENDPOINT_URL=https://example.com")},
	}, {
		// A denied variable set on a line of its own, the guide's own idiom for
		// the region: no line of the sequence names the CLI, and the read below it
		// still reads the variable's endpoint.
		name:    "an endpoint exported on a line of its own",
		mutated: appended("export AWS_ENDPOINT_URL=https://example.com", "aws ec2 describe-instances"),
		want:    []string{at(plantedLine, "AWS_ENDPOINT_URL=https://example.com")},
	}, {
		name:    "a certificate bundle exported on a line of its own",
		mutated: appended("export AWS_CA_BUNDLE=./corp.pem", "aws ec2 describe-instances"),
		want:    []string{at(plantedLine, "AWS_CA_BUNDLE=./corp.pem")},
	}, {
		name:    "an impersonation exported on a line of its own",
		mutated: appended("export CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT=sa@example-project.iam.gserviceaccount.com", "gcloud compute instances list"),
		want:    []string{at(plantedLine, "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT=sa@example-project.iam.gserviceaccount.com")},
	}, {
		name:    "an active gcloud configuration exported on a line of its own",
		mutated: appended("export CLOUDSDK_ACTIVE_CONFIG_NAME=shop", "gcloud compute instances list"),
		want:    []string{at(plantedLine, "CLOUDSDK_ACTIVE_CONFIG_NAME=shop")},
	}, {
		name:    "an az connection verification disabled on a line of its own",
		mutated: appended("export AZURE_CLI_DISABLE_CONNECTION_VERIFICATION=true", "az vm list-sizes"),
		want:    []string{at(plantedLine, "AZURE_CLI_DISABLE_CONNECTION_VERIFICATION=true")},
	}, {
		// A denied flag on a line of its own: the argument it follows spans
		// physical lines without a trailing `\`, so the flag belongs to no CLI
		// name's field sequence, and the text-wide rule is what refuses it.
		name: "a denied flag after a multi-line quote",
		mutated: appended("```bash",
			"aws ec2 describe-instances --query '",
			"  Reservations[].Instances[].InstanceId",
			"}' --no-verify-ssl > \"$out/x.json\"",
			"```"),
		want: []string{at(plantedLine+3, "--no-verify-ssl")},
	}, {
		// A single quote is no field separator: the word a denied flag arrives
		// with must be trimmed before the flag is read.
		name:    "a denied flag in a bash -c argument that closes its quote",
		mutated: appended(`sh -c 'aws ec2 describe-instances --no-verify-ssl'`),
		want:    []string{at(plantedLine, "aws ec2 describe-instances --no-verify-ssl")},
	}, {
		name:    "a denied flag the invocation quotes",
		mutated: appended(`aws ec2 describe-instances '--endpoint-url' https://example.com`),
		want:    []string{at(plantedLine, "aws ec2 describe-instances '--endpoint-url'")},
	}, {
		name:    "an az rest whose url flag is quoted",
		mutated: fence("```bash", "```", "az rest --method get '--url' https://example.com/x"),
		want:    []string{at(plantedLine+1, "az rest")},
	}, {
		// A read that ends at its line's last field takes the next line's fields,
		// even though the words so far already read: the words that make it no
		// read may be on the line the text wraps onto.
		name:    "a read whose denying flag a code span wraps",
		mutated: appended("Point it at LocalStack: `aws ec2 describe-instances", "--endpoint-url http://localhost:4566`."),
		want:    []string{at(plantedLine, "aws ec2 describe-instances --endpoint-url")},
	}, {
		name:    "an az rest whose second method a code span wraps",
		mutated: appended("Or `az rest --method get --url https://management.azure.com/x", "--method post`."),
		want:    []string{at(plantedLine, "az rest")},
	}, {
		name:    "an az rest whose address a code span wraps",
		mutated: appended("Or `az rest --method get --url", "https://example.com/x`."),
		want:    []string{at(plantedLine, "az rest")},
	}, {
		// A CRLF line break: the fields end at the name, and the write is on the
		// carriage return's next line.
		name:    "a crlf line break after the cli's name",
		mutated: appended("Tag it by hand with `aws\r", "ec2 create-tags --resources i-0123456789abcdef0`."),
		want:    []string{at(plantedLine, "aws ec2 create-tags")},
	}, {
		// The field scan splits `=` apart, so the scan refuses an inline method
		// without azRestReads' inline branch; the branch is a defence for reads on
		// unsplit words.
		name:    "an az rest whose method is written with an equals sign",
		mutated: fence("```bash", "```", "az rest --method=post --url https://management.azure.com/x"),
		want:    []string{at(plantedLine+1, "az rest --method")},
	}, {
		// az rest: one GET is what decides, and an address, a resource, a body or a
		// method the check would not read hide a write in a read's clothes.
		name:    "an az rest that addresses another API",
		mutated: fence("```bash", "```", "az rest --method get --url https://example.com/x"),
		want:    []string{at(plantedLine+1, "az rest")},
	}, {
		name:    "an az rest that passes a resource",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --resource https://graph.microsoft.com"),
		want:    []string{at(plantedLine+1, "az rest")},
	}, {
		name:    "an az rest whose method is written as a short flag with its value",
		mutated: fence("```bash", "```", "az rest -mpost --url https://management.azure.com/x"),
		want:    []string{at(plantedLine+1, "az rest -mpost")},
	}, {
		name:    "an az rest whose method flag is truncated",
		mutated: fence("```bash", "```", "az rest --meth put --url https://management.azure.com/x"),
		want:    []string{at(plantedLine+1, "az rest --meth")},
	}, {
		// A read that points the CLI somewhere else, feeds it input of the
		// caller's choosing or turns its checks off writes to a cloud even where
		// its command path reads — in full or as the prefix the parser completes.
		name:    "an aws read that names another endpoint",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --endpoint-url https://example.com"),
		want:    []string{at(plantedLine+1, "aws ec2 describe-instances --endpoint-url")},
	}, {
		name:    "an aws read whose endpoint flag is truncated",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --endpoint https://example.com"),
		want:    []string{at(plantedLine+1, "aws ec2 describe-instances --endpoint")},
	}, {
		name:    "an aws read that skips certificate verification",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --no-verify-ssl"),
		want:    []string{at(plantedLine+1, "aws ec2 describe-instances --no-verify-ssl")},
	}, {
		name:    "an aws read that trusts another certificate bundle",
		mutated: fence("```bash", "```", "aws ec2 describe-instances --ca-bundle ./ca.pem"),
		want:    []string{at(plantedLine+1, "aws ec2 describe-instances --ca-bundle")},
	}, {
		name:    "a gcloud read that impersonates a service account",
		mutated: fence("```bash", "```", "gcloud compute instances list --impersonate-service-account sa@example-project.iam.gserviceaccount.com"),
		want:    []string{at(plantedLine+1, "gcloud compute instances list --impersonate-service-account")},
	}, {
		name:    "a gcloud read whose impersonation flag is truncated",
		mutated: fence("```bash", "```", "gcloud compute instances list --impersonate sa@example-project.iam.gserviceaccount.com"),
		want:    []string{at(plantedLine+1, "gcloud compute instances list --impersonate")},
	}, {
		name:    "an az rest whose resource flag is truncated",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --resou https://graph.microsoft.com"),
		want:    []string{at(plantedLine+1, "az rest --method get --url https://management.azure.com/x --resou")},
	}, {
		name:    "an az rest whose headers flag is truncated",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --head x=y"),
		want:    []string{at(plantedLine+1, "az rest --method get --url https://management.azure.com/x --head")},
	}, {
		name:    "an az rest whose body flag is truncated",
		mutated: fence("```bash", "```", "az rest --method get --url https://management.azure.com/x --bod {}"),
		want:    []string{at(plantedLine+1, "az rest --method get --url https://management.azure.com/x --bod")},
	}, {
		// A variable that holds the CLI's name, the residual: the scan sees the
		// assignment's value on this line, and nothing on a line of its own.
		name:    "a CLI a variable names",
		mutated: fence("```bash", "```", `cli=aws; "$cli" ec2 create-tags --resources i-0123456789abcdef0`),
		want:    []string{at(plantedLine+1, "aws cli ec2 create-tags")},
	}, {
		name:    "a fence label the check does not know",
		mutated: fence("```powershell", "```", "Get-EC2Instance -Region eu-central-1"),
		want:    []string{fmt.Sprintf(":%d: the fence is labelled %q", plantedLine, "powershell")},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scan := scanText(inventoryGuidePath, tc.mutated)

			if len(scan.problems) != len(tc.want) {
				t.Errorf("the field scan of the guide with %s reports %d problems, want %d: %v",
					tc.name, len(scan.problems), len(tc.want), scan.problems)
			}

			for _, want := range tc.want {
				if !slices.ContainsFunc(scan.problems, func(problem string) bool {
					return strings.Contains(problem, want)
				}) {
					t.Errorf("the scan of the guide with %s reports no problem that holds %q: %v",
						tc.name, want, scan.problems)
				}
			}
		})
	}
}

// TestCloudReadDecisions pins the decision each CLI's read commands decide: the
// words the check holds a sequence to and whether it reads. The lists themselves
// are cloudCLIs, the one place a read command is written down, so a new guide
// command is a single edit there. The cases around a read are the ones the lists
// must refuse: a positional argument that ends in a read word, a read that returns
// a secret, a flag that points the CLI elsewhere, a flag spelt as the prefix the
// parser completes, `az rest` with one method, with GET, with two, with an
// abbreviated method flag and with an address beyond the management endpoint, and a
// global flag before the command path, which is no read on purpose.
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
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "as", "well."}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "evil"}, verb: "rest --url evil"},
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
		// A denied flag spelt as a prefix the CLIs' parsers complete, and the flags
		// that point a CLI at input of the caller's choosing.
		{cli: "aws", words: []string{"ec2", "describe-instances", "--endpoint", "https://example.com"}, verb: "ec2 describe-instances --endpoint"},
		{cli: "aws", words: []string{"ec2", "describe-instances", "--cli-input-json", "./shop.json"}, verb: "ec2 describe-instances --cli-input-json"},
		{cli: "aws", words: []string{"ec2", "describe-instances", "--cli-input-yaml", "./shop.yaml"}, verb: "ec2 describe-instances --cli-input-yaml"},
		// A single quote is no field separator, so a denied flag or an az rest flag
		// reaches reads with the quote still on it, and reads trims it.
		{cli: "aws", words: []string{"ec2", "describe-instances", "--no-verify-ssl'"}, verb: "ec2 describe-instances --no-verify-ssl"},
		{cli: "aws", words: []string{"ec2", "describe-instances", "'--endpoint-url'", "https://example.com"}, verb: "ec2 describe-instances --endpoint-url"},
		{cli: "az", words: []string{"rest", "--method", "get", "'--url'", "https://example.com/x"}, verb: "rest --url https://example.com/x"},
		{cli: "az", words: []string{"rest", "'--method'", "post", "--url", "https://management.azure.com/x"}, verb: "rest --method post"},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x", "--resou", "https://graph.microsoft.com"}, verb: "rest --resou"},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x", "--head", "x=y"}, verb: "rest --head"},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x", "--bod", "{}"}, verb: "rest --bod"},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--impersonate", "sa@example-project.iam.gserviceaccount.com"}, verb: "compute instances list --impersonate"},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--flags-file", "./flags.yaml"}, verb: "compute instances list --flags-file"},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--access-token-file", "./token"}, verb: "compute instances list --access-token-file"},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--configuration", "shop"}, verb: "compute instances list --configuration"},
		// A global flag before the command path is no read on purpose: a parser
		// that reads the path past a flag could be run past a denied one.
		{cli: "aws", words: []string{"--output", "json", "ec2", "describe-instances"}, verb: ""},
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

// fieldScan is what one text's field scan found: how many CLI-name fields each
// CLI's read commands accepted, and a problem for every field sequence they
// refused. The counts are what the floors hold.
type fieldScan struct {
	// accepted counts the CLI-name fields reads accepted, per CLI name.
	accepted map[string]int
	// problems are the lines the scan refuses, in the text's order.
	problems []string
}

// scanText runs the read-only check over every line of a text: the guide's, or a
// program's. It is the check's one mechanism, and it trusts no Markdown and no
// shell reading: every line is split into fields wherever it sits.
func scanText(file, text string) fieldScan {
	physical := strings.Split(text, "\n")

	scan := fieldScan{accepted: make(map[string]int)}
	scan.problems = fenceLabelProblems(file, physical)

	lines := joinContinuations(physical)

	for at, line := range lines {
		fields := lineFields(line.text)

		for i, field := range fields {
			name := cliNameField(field)
			if name == "" {
				continue
			}

			cli, known := cloudCLIByName(name)
			if !known {
				continue
			}

			if assignment, flag, found := cli.environmentOverride(line.text); found {
				scan.problems = append(scan.problems, environmentRefusal(file, line.number, assignment, flag, name, cli))

				continue
			}

			words := fields[i+1:]

			// A line ends at its last field, and a line that does takes the next
			// line's fields as the sequence's continuation: a comment table, a
			// prose sentence or a fence body wraps a command there.
			if at+1 < len(lines) && endsWithLastField(line.text, fields) {
				switch {
				case len(words) == 0:
					words = continuationFields(lines[at+1].text)
				default:
					if _, read := cli.reads(words); !read {
						words = append(slices.Clone(words), continuationFields(lines[at+1].text)...)
					}
				}
			}

			if len(words) == 0 {
				// A bare mention: the name stands alone on its line, as the
				// guide's JSON writes `"source": "aws",`, whose comma ends the
				// mention rather than the array below it.
				continue
			}

			if slices.Contains(guideBareMentions, path.Base(words[0])) {
				continue
			}

			held, read := cli.reads(words)
			if read {
				scan.accepted[name]++

				continue
			}

			scan.problems = append(scan.problems, refusal(file, line.number, name, words, held))
		}
	}

	return scan
}

// refusal is the problem a CLI-name field and the fields after it get when they
// are no read: the file and the line, the command as the scan read it, what the
// check held it to, and the reference the CLI's read commands come from.
func refusal(file string, number int, name string, fields []string, held string) string {
	cli, _ := cloudCLIByName(name)

	return fmt.Sprintf("%s:%d: %s is not a read the guide may name (CLAUDE.md rule 1): cloudCLIs holds it to %q, and of %s the check reads only an exact command path, `az rest` as one GET of the management endpoint, and no denied flag; cloudCLIs lists the read commands of %s, and their reference, at %s",
		file, number, strings.TrimSpace(name+" "+strings.Join(fields, " ")), held, cli.name, cli.name, cli.reference)
}

// environmentRefusal is the problem a line that sets a denied variable gets when it
// names the CLI whose variable it sets: the variable does what the flag does.
func environmentRefusal(file string, number int, assignment, flag, name string, cli cloudCLI) string {
	return fmt.Sprintf("%s:%d: %s sets in the environment what %s denies, on a line that names %s, so the line is no read the guide may name (CLAUDE.md rule 1); cloudCLIs lists the read commands of %s, and their reference, at %s",
		file, number, assignment, flag, name, cli.name, cli.reference)
}

// subcommandPath returns the words of a sequence that name its command groups and
// its command, up to its first flag: `az servicebus queue list --namespace-name x`
// has the path servicebus, queue, list.
func subcommandPath(words []string) []string {
	for i, word := range words {
		if strings.HasPrefix(word, "-") {
			return words[:i]
		}
	}

	return words
}

// logicalLine is one line of a text after the lines a trailing `\` continues have
// been joined onto it, with the 1-based number of the physical line it starts on.
type logicalLine struct {
	number int
	text   string
}

// joinContinuations joins every line a trailing `\` continues onto the line it
// continues: a denied flag or a second `az rest --method` on a continuation line
// then stands in the sequence it belongs to.
func joinContinuations(lines []string) []logicalLine {
	var joined []logicalLine

	for at := 0; at < len(lines); at++ {
		line := logicalLine{number: at + 1, text: lines[at]}

		for endsWithContinuation(line.text) {
			line.text = strings.TrimSuffix(strings.TrimRight(line.text, " \t"), "\\")
			at++

			if at == len(lines) {
				break
			}

			line.text += " " + lines[at]
		}

		joined = append(joined, line)
	}

	return joined
}

// endsWithContinuation reports whether a line ends with the backslash that
// continues it on the next one, white space aside.
func endsWithContinuation(line string) bool {
	return strings.HasSuffix(strings.TrimRight(line, " \t"), "\\")
}

// continuationFields returns the fields of a line that continues the one above it:
// its blockquote marker, its comment marker and its indentation are no part of the
// command the line goes on with.
func continuationFields(line string) []string {
	return lineFields(strings.TrimLeft(line, " \t>#"))
}

// endsWithLastField reports whether a line's text ends at its last field, rather
// than in punctuation or a closing quote: it is the guard a wrapped command needs,
// so the guide's `"source": "aws",` does not read the array below it as a command,
// while a line that ends at `az servicebus topic` reads `subscription list` as its
// continuation.
func endsWithLastField(text string, fields []string) bool {
	if len(fields) == 0 {
		return false
	}

	return strings.HasSuffix(strings.TrimRight(text, " \t\\"), fields[len(fields)-1])
}

// guideBareMentions are the words today's guide and programs write after a cloud
// CLI's name when they name the CLI and run no command. A bare mention is narrow on
// purpose: a CLI name followed by any other word fails the check rather than passing
// as prose, so a command the scan could not read cannot hide behind one. Each word
// is here for a line of today's text:
//
//   - ".", the full stop after the directory the guide empties: `rm -r inventories/aws`.
//   - "CLI", aws.jq's first line: "the read-only aws CLI calls".
//   - "a", the guide's note on the projections: "`--query` for `aws` and `az`, a `--format`".
//   - "and", the same sentence: "for `aws` and `az`".
//   - "call", its note on the quiet flag: "Every `gcloud` call above carries `--quiet`".
//   - "calls", azure.jq's and gcp.jq's first lines: "the read-only az calls".
//   - "gcp", the guide's source table: "`aws`, `gcp` or `azure` here".
//   - "or", the guide's requirements: "the provider's CLI (`aws` version 2, `gcloud`, or `az` …)".
//   - "pages", the note on the regions: "the gcloud" (inventory.md:1119) "pages
//     document no "all regions" value" (inventory.md:1120).
//   - "so", the note on the projections: "for `gcloud`), so it runs once".
//   - "version", the requirements line: "`aws` version 2".
//   - "with", the requirements line: "`az` with the `resource-graph` extension".
//   - "writes", gcp.jq's last comment: "as gcloud writes a string".
var guideBareMentions = []string{
	".", "CLI", "a", "and", "call", "calls", "gcp", "or", "pages", "so", "version", "with", "writes",
}

// lineFieldSeparators are the characters the field scan splits a line into fields
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

// isCloudCLIName reports whether the word is one of the three cloud CLIs' names.
func isCloudCLIName(word string) bool {
	return slices.ContainsFunc(cloudCLIs, func(cli cloudCLI) bool { return cli.name == word })
}

// guideFenceLanguages are the labels the guide's fences may carry: the shell
// languages its command blocks are written in, the prose languages of its samples,
// and no label at all. A label on neither list fails the check, so a block in a
// language the scan knows nothing of is a deliberate edit here — the scan reads
// every fence's lines whatever the label, but a new language is worth a look.
var guideFenceLanguages = []string{"", "bash", "console", "json", "sh", "shell", "text", "zsh"}

// fenceLabelProblems returns a problem for every fence opener whose label is on no
// list of the guide's fence languages.
func fenceLabelProblems(file string, lines []string) []string {
	var problems []string

	for at, line := range lines {
		label, opens := fenceLabel(line)
		if !opens || slices.Contains(guideFenceLanguages, label) {
			continue
		}

		problems = append(problems, fmt.Sprintf("%s:%d: the fence is labelled %q, which is on no list of the guide's fence languages (%s), so a block the scan knows no language of could hold commands: label it as the shell text it is, or add its language to guideFenceLanguages on purpose",
			file, at+1, label, strings.Join(guideFenceLanguages, ", ")))
	}

	return problems
}

// fenceLabel returns the first word of a fence's info string and whether the line
// opens a fence: a run of at least three backticks or tildes, indented by at most
// three spaces, and — for a backtick fence — an info string with no backtick, as
// CommonMark has it.
func fenceLabel(line string) (string, bool) {
	if len(line)-len(strings.TrimLeft(line, " ")) > 3 {
		return "", false
	}

	line = strings.TrimRight(line, " \t\r")

	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return "", false
	}

	run := 0

	for run < len(line) && line[run] == line[0] {
		run++
	}

	if run < 3 {
		return "", false
	}

	info := strings.TrimSpace(line[run:])
	if line[0] == '`' && strings.ContainsRune(info, '`') {
		return "", false
	}

	return leadingWord(info), true
}

// leadingWord returns the first word of a text, "" for a text with no word.
func leadingWord(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

// shippedProgram is one program in the guide's programs directory, with its field
// scan and its path relative to the repository root.
type shippedProgram struct {
	path string
	scan fieldScan
}

// shippedPrograms returns the programs the guide ships, each scanned for cloud CLI
// names. It fails the test for a file beside them that is no `.jq` program: a
// script added there — a shell helper, for example — holds commands this check must
// read, so it has to be read here on purpose rather than skipped.
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
		scan := scanText(programPath, string(readRepositoryFile(t, programPath)))

		for _, problem := range scan.problems {
			t.Error(problem)
		}

		programs = append(programs, shippedProgram{path: programPath, scan: scan})
	}

	return programs
}
