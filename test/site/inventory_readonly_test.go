package site_test

import (
	"fmt"
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
// them at once: guideInvocations reads the invocations out of the guide's command
// blocks, through this package's own fencedBashBlocks and headingText (the
// extraction the inventory page's checks use, so the guide has one parser), and
// out of the programs the guide ships, and notReadOnly holds each invocation's
// verb to its CLI's read verbs.
//
// The check is a function over text rather than over the file, so
// TestReadOnlyCheckFlagsPlantedWrites can plant a write into a copy of the guide
// in memory and see the check report it: a check that never bites would pass the
// guide today and catch nothing tomorrow.

// inventoryGuidePath is the guide this file's checks read, relative to the
// repository root.
const inventoryGuidePath = "docs/guide/inventory.md"

// cloudCLIs are the three cloud CLIs the guide tells a visitor to run, each with
// the read verbs its invocations may use and the reference those verbs come
// from. The lists are as narrow as the guide's commands allow: a command whose
// verb is not on its CLI's list fails the check, so a new read verb is a
// deliberate edit of the list rather than something that slips in.
var cloudCLIs = []cloudCLI{{
	name: "aws",
	// AWS CLI command reference: a service's read operations are its
	// Describe*, List* and Get* ones (batch-get-* among them), and AWS Config's
	// SelectResourceConfig, the guide's SQL-like read-only query, is no
	// exception.
	readVerbs: "describe-*, list-*, get-*, batch-get-*, and configservice select-resource-config",
	reference: "https://awscli.amazonaws.com/v2/documentation/api/latest/reference/index.html",
	readVerb:  awsReadVerb,
}, {
	name: "az",
	// Azure CLI reference: `list`, `show` and `list-sizes` are the read verbs;
	// `az graph query` reads Resource Graph, and `az rest --method get` makes an
	// HTTP GET, the only method the guide may pass.
	readVerbs: "a list, show or list-sizes subcommand, az graph query, and az rest --method get",
	reference: "https://learn.microsoft.com/en-us/cli/azure/reference-index",
	readVerb:  azReadVerb,
}, {
	name: "gcloud",
	// gcloud reference: `list` is the only verb the guide uses to read, and
	// `gcloud asset search-all-resources` is its Cloud Asset Inventory read.
	readVerbs: "a list subcommand, and asset search-all-resources",
	reference: "https://cloud.google.com/sdk/gcloud/reference",
	readVerb:  gcloudReadVerb,
}}

// cloudCLI is one of the cloud CLIs the guide runs.
type cloudCLI struct {
	// name is the program's name: the word an invocation starts with.
	name string
	// readVerbs describes the verbs an invocation of the CLI may use, for a
	// failure message.
	readVerbs string
	// reference is the CLI's command reference, the source of readVerbs.
	reference string
	// readVerb returns the word that decides whether the invocation reads, and
	// whether that word is a read verb.
	readVerb func(words []string) (string, bool)
}

// cloudCLIByName returns the CLI the name stands for, failing the test for a name
// none of the three has: a table that named a CLI the check does not know would
// otherwise read nothing.
func cloudCLIByName(t *testing.T, name string) cloudCLI {
	t.Helper()

	for _, cli := range cloudCLIs {
		if cli.name == name {
			return cli
		}
	}

	t.Fatalf("%q is no CLI this check knows, want one of the guide's: %v", name, cloudCLIs)

	return cloudCLI{}
}

// cloudCommand is one invocation of a cloud CLI in a text: the CLI, the words it
// is given, and where it was found.
type cloudCommand struct {
	// cli is the CLI's name: "aws", "az" or "gcloud".
	cli string
	// words are the words of the invocation, the CLI's own name left out.
	words []string
	// where names the file and the place in it the invocation was found.
	where string
}

// String renders the invocation as the guide writes it.
func (c cloudCommand) String() string {
	return strings.TrimSpace(c.cli + " " + strings.Join(c.words, " "))
}

// write is one cloud CLI invocation the read-only check refuses: where it was
// found, the command itself, and the command path the check held to the read
// verbs.
type write struct {
	where   string
	cli     string
	command string
	path    string
}

// String renders the write with the read verbs it should have used and where
// those come from, for a failure message.
func (w write) String() string {
	return fmt.Sprintf("%s: %s names no read verb in %q, and the guide may only read (CLAUDE.md rule 1): %s's read verbs are %s, from %s",
		w.where, w.command, w.path, w.cli, cloudCLIByNameNoTest(w.cli).readVerbs, cloudCLIByNameNoTest(w.cli).reference)
}

// cloudCLIByNameNoTest returns the CLI the name stands for, or a CLI with only
// the name set for a name none has: String runs in a failure message, where no
// testing.T is at hand, and notReadOnly only ever holds names of cloudCLIs.
func cloudCLIByNameNoTest(name string) cloudCLI {
	for _, cli := range cloudCLIs {
		if cli.name == name {
			return cli
		}
	}

	return cloudCLI{name: name}
}

// TestInventoryGuideCommandsAreReadOnly holds every `aws`, `az` and `gcloud`
// command of the inventory guide, and of the programs it ships, to its CLI's read
// verbs (issue #716, CLAUDE.md rule 1). The floors below are what keeps the check
// from passing over an extraction that found nothing: each CLI's commands are in
// the guide today, and a parser that quietly lost them fails here.
func TestInventoryGuideCommandsAreReadOnly(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, inventoryGuidePath))
	commands := guideInvocations(t, guide)

	// The floors are below what the guide holds today (36 aws, 21 az and 25
	// gcloud invocations), so the guide may lose a command without failing here,
	// while a parser that broke fails at once.
	floors := []struct {
		cli     string
		atLeast int
	}{
		{cli: "aws", atLeast: 30},
		{cli: "az", atLeast: 15},
		{cli: "gcloud", atLeast: 20},
	}

	for _, floor := range floors {
		t.Run("finds the "+floor.cli+" commands", func(t *testing.T) {
			t.Parallel()

			found := 0

			for _, command := range commands {
				if command.cli == floor.cli {
					found++
				}
			}

			if found < floor.atLeast {
				t.Errorf("%s holds %d %s invocations, want at least %d, so this check read something: the extraction reads the guide's command blocks and its programs, and a parser that finds nothing would pass every check below over an empty list",
					inventoryGuidePath, found, floor.cli, floor.atLeast)
			}
		})
	}

	for _, w := range notReadOnly(t, commands) {
		t.Error(w.String())
	}
}

// TestReadOnlyCheckFlagsPlantedWrites shows the check bites (issue #716): a write
// planted into a copy of the guide, in memory, is reported and the rest of the
// guide is not. Each case appends its command to the guide text as a bash block
// of its own, so out is exactly the one write the case planted.
func TestReadOnlyCheckFlagsPlantedWrites(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, inventoryGuidePath))

	// A planted block lands in the guide's last section, which is this one.
	const lastSection = `Validate the result`

	tests := []struct {
		name    string
		planted string
		want    write
	}{{
		name:    "a planted aws write",
		planted: "aws ec2 create-tags --resources i-0123456789abcdef0 --tags Key=Name,Value=shop-web",
		want: write{
			where:   inventoryGuidePath + `, section "` + lastSection + `"`,
			cli:     "aws",
			command: "aws ec2 create-tags --resources i-0123456789abcdef0 --tags Key=Name,Value=shop-web",
			path:    "ec2 create-tags",
		},
	}, {
		name:    "a planted az write",
		planted: "az group delete --name shop-web --yes",
		want: write{
			where:   inventoryGuidePath + `, section "` + lastSection + `"`,
			cli:     "az",
			command: "az group delete --name shop-web --yes",
			path:    "group delete",
		},
	}, {
		name:    "a planted gcloud write",
		planted: "gcloud compute instances delete shop-web --zone=europe-west1-b --quiet",
		want: write{
			where:   inventoryGuidePath + `, section "` + lastSection + `"`,
			cli:     "gcloud",
			command: "gcloud compute instances delete shop-web --zone=europe-west1-b --quiet",
			path:    "compute instances delete shop-web",
		},
	}, {
		name:    "a planted az rest that is no GET",
		planted: "az rest --method post --url https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/shop-web",
		want: write{
			where:   inventoryGuidePath + `, section "` + lastSection + `"`,
			cli:     "az",
			command: "az rest --method post --url https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/shop-web",
			path:    "rest --method post",
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mutated := guide + "\n\n```bash\n" + tc.planted + "\n```\n"

			got := notReadOnly(t, guideInvocations(t, mutated))
			want := []write{tc.want}

			if diff := cmp.Diff(want, got, cmp.AllowUnexported(write{})); diff != "" {
				t.Errorf("the read-only check over a copy of the guide with %q planted differs from the one write it holds (-want +got):\n%s",
					tc.planted, diff)
			}
		})
	}
}

// TestCloudReadVerbs pins the read verbs of each CLI, so the allow list stays as
// narrow as the guide needs and a table that widened it fails here: a read verb
// the guide never uses is a verb no review looked at.
func TestCloudReadVerbs(t *testing.T) {
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
		{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, verb: "ec2 create-tags"},
		{cli: "aws", words: []string{"ec2", "delete-volume", "--volume-id", "vol-0123456789abcdef0"}, verb: "ec2 delete-volume"},
		{cli: "aws", words: []string{"ec2"}, verb: "ec2"},
		{cli: "aws", words: []string{"ec2", "describe-instances", "--output", "json"}, verb: "ec2 describe-instances", read: true},
		{cli: "aws", words: []string{"configservice", "select-aggregate-resource-config"}, verb: "configservice select-aggregate-resource-config"},
		{cli: "az", words: []string{"account", "show", "--query", "id"}, verb: "account show", read: true},
		{cli: "az", words: []string{"servicebus", "topic", "subscription", "list", "--namespace-name", "x"}, verb: "servicebus topic subscription list", read: true},
		{cli: "az", words: []string{"graph", "query", "--graph-query", "Resources"}, verb: "graph query", read: true},
		{cli: "az", words: []string{"rest", "--method", "get", "--url", "https://management.azure.com/x"}, verb: "rest --method get", read: true},
		{cli: "az", words: []string{"rest", "--method=post", "--url", "https://management.azure.com/x"}, verb: "rest --method post"},
		{cli: "az", words: []string{"rest", "--url", "https://management.azure.com/x"}, verb: "rest"},
		{cli: "az", words: []string{"group", "delete", "--name", "shop-web"}, verb: "group delete"},
		{cli: "az", words: []string{"vm", "create", "--name", "shop-web"}, verb: "vm create"},
		{cli: "gcloud", words: []string{"compute", "instances", "list", "--project=x"}, verb: "compute instances list", read: true},
		{cli: "gcloud", words: []string{"asset", "search-all-resources", "--scope=projects/x"}, verb: "asset search-all-resources", read: true},
		{cli: "gcloud", words: []string{"compute", "instances", "delete", "shop-web"}, verb: "compute instances delete shop-web"},
		{cli: "gcloud", words: []string{"compute", "instances", "create", "shop-web"}, verb: "compute instances create shop-web"},
	}

	for _, tc := range tests {
		t.Run(tc.cli+" "+strings.Join(tc.words, " "), func(t *testing.T) {
			t.Parallel()

			verb, read := cloudCLIByName(t, tc.cli).readVerb(tc.words)

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
// write in any of those shapes reaches the check. The text of each case is the
// shape the guide uses today.
func TestCloudInvocationsFindEveryShape(t *testing.T) {
	t.Parallel()

	const where = "a command block"

	tests := []struct {
		name string
		text string
		want []cloudCommand
	}{{
		name: "a command in a list piped to xargs",
		text: "jq -r '.DomainNames[].DomainName' \"$out/opensearch-domain-names.json\" |\n" +
			"  xargs -r -n 5 aws opensearch describe-domains --output json --domain-names\n",
		want: []cloudCommand{{cli: "aws", words: []string{"opensearch", "describe-domains", "--output", "json", "--domain-names"}, where: where}},
	}, {
		name: "a command substitution inside a quoted word",
		text: `subscription="$(az account show --query id --output tsv)"`,
		want: []cloudCommand{{cli: "az", words: []string{"account", "show", "--query", "id", "--output", "tsv"}, where: where}},
	}, {
		name: "two commands joined by &&",
		text: "true && gcloud compute instances list --project=example-project && echo done",
		want: []cloudCommand{{cli: "gcloud", words: []string{"compute", "instances", "list", "--project=example-project"}, where: where}},
	}, {
		name: "two commands joined by a semicolon",
		text: "aws ec2 describe-volumes; aws ec2 create-tags --resources i-0123456789abcdef0",
		want: []cloudCommand{
			{cli: "aws", words: []string{"ec2", "describe-volumes"}, where: where},
			{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where},
		},
	}, {
		name: "a command wrapped by a line continuation",
		text: "aws ec2 describe-snapshots --owner-ids self --output json \\\n" +
			"  --query '{Snapshots: Snapshots[].{VolumeId: VolumeId}}' > \"$out/ec2-snapshots.json\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-snapshots", "--owner-ids", "self", "--output", "json", "--query", "{Snapshots: Snapshots[].{VolumeId: VolumeId}}"}, where: where}},
	}, {
		name: "a quoted filter that spans lines",
		text: "aws ec2 describe-volumes --output json --query '{Volumes: Volumes[].{\n" +
			"  VolumeId: VolumeId, Size: Size}}' \\\n" +
			"  > \"$out/ec2-volumes.json\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-volumes", "--output", "json", "--query", "{Volumes: Volumes[].{\n  VolumeId: VolumeId, Size: Size}}"}, where: where}},
	}, {
		name: "a double-quoted argument whose braces and pipes are no operator",
		text: `az graph query --graph-query "Resources | where type =~ 'microsoft.compute/disks'"`,
		want: []cloudCommand{{cli: "az", words: []string{"graph", "query", "--graph-query", "Resources | where type =~ 'microsoft.compute/disks'"}, where: where}},
	}, {
		name: "a comment that names a command",
		text: "# aws ec2 create-tags --resources i-0123456789abcdef0\naws ec2 describe-volumes\n",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-volumes"}, where: where}},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := invocationsIn(tc.text, true, where)

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(cloudCommand{})); diff != "" {
				t.Errorf("the invocations of %q differ from the ones it holds (-want +got):\n%s", tc.text, diff)
			}
		})
	}
}

// notReadOnly holds every invocation to its CLI's read verbs and returns the ones
// that are no read.
func notReadOnly(t *testing.T, commands []cloudCommand) []write {
	t.Helper()

	var writes []write

	for _, command := range commands {
		verb, read := cloudCLIByName(t, command.cli).readVerb(command.words)
		if read {
			continue
		}

		writes = append(writes, write{
			where:   command.where,
			cli:     command.cli,
			command: command.String(),
			path:    verb,
		})
	}

	return writes
}

// awsReadVerb returns the command path an aws invocation names, and whether it
// is a read: `aws <service> <operation>`, so the operation is the second word of
// the path, whatever service it belongs to.
func awsReadVerb(words []string) (string, bool) {
	path := subcommandPath(words)
	held := strings.Join(path, " ")

	if len(path) < 2 {
		return held, false
	}

	operation := path[1]

	for _, prefix := range []string{"describe-", "list-", "get-", "batch-get-"} {
		if strings.HasPrefix(operation, prefix) {
			return held, true
		}
	}

	return held, path[0] == "configservice" && operation == "select-resource-config"
}

// azReadVerb returns the command path an az invocation names, and whether it is
// a read: its last command word is `list`, `show` or `list-sizes`, `az graph
// query` reads Resource Graph, and `az rest` reads only with an HTTP GET.
func azReadVerb(words []string) (string, bool) {
	path := subcommandPath(words)
	held := strings.Join(path, " ")

	if len(path) == 0 {
		return held, false
	}

	switch {
	case slices.Equal(path, []string{"graph", "query"}):
		return held, true
	case slices.Equal(path, []string{"rest"}):
		method := azMethod(words)
		if method == "" {
			return held, false
		}

		return held + " --method " + method, method == "get"
	}

	switch path[len(path)-1] {
	case "list", "show", "list-sizes":
		return held, true
	}

	return held, false
}

// azMethod returns the HTTP method an `az rest` invocation passes, as --method
// get or --method=get, or "" for one that passes none.
func azMethod(words []string) string {
	for i, word := range words {
		if method, ok := strings.CutPrefix(word, "--method="); ok {
			return method
		}

		if (word == "--method" || word == "-m") && i+1 < len(words) {
			return words[i+1]
		}
	}

	return ""
}

// gcloudReadVerb returns the command path a gcloud invocation names, and whether
// it is a read: its last command word is `list`, or the whole path is `asset
// search-all-resources`. A gcloud command that passes a positional argument
// where the guide's commands pass flags would be held to a word that is no verb
// and fail the list, so its author extends the list deliberately.
func gcloudReadVerb(words []string) (string, bool) {
	path := subcommandPath(words)
	held := strings.Join(path, " ")

	if slices.Equal(path, []string{"asset", "search-all-resources"}) {
		return held, true
	}

	if len(path) == 0 {
		return held, false
	}

	return held, path[len(path)-1] == "list"
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

// guideInvocations returns every aws, az and gcloud invocation of the guide text
// and of the programs the guide ships.
//
// The command blocks are read through this package's fencedBashBlocks, one
// section per heading, so the guide keeps one parser: the inventory page's checks
// read the same blocks the same way. The programs are read from the repository,
// since a shipped script is read-only today and a write in one is a write the
// guide ships.
func guideInvocations(t *testing.T, guide string) []cloudCommand {
	t.Helper()

	var commands []cloudCommand

	seen := map[string]bool{}

	for _, line := range strings.Split(guide, "\n") {
		heading := headingText(line)
		if heading == "" || seen[heading] {
			continue
		}

		seen[heading] = true

		for _, block := range fencedBashBlocks(t, guide, heading) {
			commands = append(commands, invocationsIn(strings.Join(block, "\n"), true, fmt.Sprintf("%s, section %q", inventoryGuidePath, heading))...)
		}
	}

	for _, source := range guideSources {
		commands = append(commands, scriptInvocations(t, guideProgramsDirectory+source.key+".jq")...)
	}

	return commands
}

// invocationsIn returns every cloud CLI invocation of a shell text, and where it
// found them: from the token that names a CLI to the operator or the CLI that
// ends it, so an invocation after a pipe, inside `$(...)`, after `&&` or after
// xargs is found like one that starts a line, and a flag's quoted argument is one
// word however many lines it spans.
func invocationsIn(text string, newlineEndsCommand bool, where string) []cloudCommand {
	tokens := shellTokens(text, newlineEndsCommand)

	var commands []cloudCommand

	for i := 0; i < len(tokens); i++ {
		cli, ok := cloudCLIName(tokens[i])
		if !ok {
			continue
		}

		var words []string

		for j := i + 1; j < len(tokens); j++ {
			if tokens[j].operator {
				break
			}

			if _, another := cloudCLIName(tokens[j]); another {
				break
			}

			words = append(words, tokens[j].text)
		}

		commands = append(commands, cloudCommand{cli: cli, words: words, where: where})
	}

	return commands
}

// scriptInvocations returns the cloud CLI invocations a program the guide ships
// names in its comments: a comment line whose first word is a CLI starts one, and
// the following comment lines that read as a subcommand continuation (a
// subcommand group, a flag) go on with it, so a command the program wraps across
// two lines is read whole. A comment line that mentions a CLI in its prose is no
// invocation and starts none.
func scriptInvocations(t *testing.T, path string) []cloudCommand {
	t.Helper()

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

	for number, line := range strings.Split(string(readRepositoryFile(t, path)), "\n") {
		content, isComment := strings.CutPrefix(strings.TrimSpace(line), "#")
		if !isComment {
			closeOpen()

			continue
		}

		content = strings.TrimSpace(content)

		fields := strings.Fields(content)

		switch {
		case len(fields) == 0:
			closeOpen()
		case isCloudCLIName(fields[0]):
			closeOpen()

			open = &cloudCommand{
				cli:   fields[0],
				words: wordTokens(strings.TrimSpace(strings.TrimPrefix(content, fields[0]))),
				where: fmt.Sprintf("%s, line %d", path, number+1),
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

// continuesSubcommand reports whether a comment line goes on with the invocation
// above it: it starts with a lower-case word, as `subscription list` and
// `blob-service-properties show` do, or with a flag. A line that starts with
// anything else, such as the JSON shape a program's comment puts beside a
// command, is its own text and ends the invocation.
func continuesSubcommand(content string) bool {
	if content == "" {
		return false
	}

	first := rune(content[0])

	return first == '-' || (first >= 'a' && first <= 'z')
}

// wordTokens returns the words of a text, its operators left out.
func wordTokens(text string) []string {
	var words []string

	for _, token := range shellTokens(text, false) {
		if !token.operator {
			words = append(words, token.text)
		}
	}

	return words
}

// shellToken is one token of a shell text: a word, or the operator that ends one.
type shellToken struct {
	text     string
	operator bool
}

// substitution is the shell context of a `$(` inside a double-quoted string: the
// words in it split as they do outside quotes, and the `)` that closes it returns
// the scan to the quotes.
const substitution = 's'

// shellTokens splits a shell text into the words a program is given and the
// operators that end them: a quote keeps its text, newlines and spaces inside it
// included, in one word; a line continuation joins two lines into one; a comment
// runs to the end of its line; and white space, a newline, a pipe, a redirection
// and a parenthesis end a word. newlineEndsCommand says whether an unquoted
// newline ends an invocation, as it does in a command block and does not in a
// comment whose command a program wraps across two lines.
func shellTokens(text string, newlineEndsCommand bool) []shellToken {
	var (
		tokens []shellToken
		word   strings.Builder
		// contexts is the stack of shell contexts the scan is in: a single
		// quote, a double quote, or a command substitution inside a double
		// quote. An empty stack is outside any quote.
		contexts []rune
	)

	context := func() rune {
		if len(contexts) == 0 {
			return 0
		}

		return contexts[len(contexts)-1]
	}

	flush := func() {
		if word.Len() == 0 {
			return
		}

		tokens = append(tokens, shellToken{text: word.String()})
		word.Reset()
	}

	operator := func(text string) {
		flush()

		tokens = append(tokens, shellToken{text: text, operator: true})
	}

	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		switch context() {
		case '\'':
			if r == '\'' {
				contexts = contexts[:len(contexts)-1]

				continue
			}

			word.WriteRune(r)

			continue
		case '"':
			switch {
			case r == '\\' && i+1 < len(runes):
				i++
				word.WriteRune(runes[i])
			case r == '"':
				contexts = contexts[:len(contexts)-1]
			case r == '$' && i+1 < len(runes) && runes[i+1] == '(':
				i++
				// A command substitution runs a program of its own, so
				// its words split as they do outside the quotes.
				flush()
				contexts = append(contexts, substitution)
			default:
				word.WriteRune(r)
			}

			continue
		}

		switch {
		case r == '\\' && i+1 < len(runes) && runes[i+1] == '\n':
			i++
		case r == '\\' && i+1 < len(runes):
			i++
			word.WriteRune(runes[i])
		case r == '\'' || r == '"':
			contexts = append(contexts, r)
		case r == '#' && word.Len() == 0:
			for i+1 < len(runes) && runes[i+1] != '\n' {
				i++
			}
		case r == ' ' || r == '\t' || r == '\r':
			flush()
		case r == '\n':
			if newlineEndsCommand {
				operator("\n")
			} else {
				flush()
			}
		default:
			if r == ')' && context() == substitution {
				contexts = contexts[:len(contexts)-1]
			}

			if r == '|' || r == '&' || r == ';' || r == '(' || r == ')' || r == '<' || r == '>' {
				operator(string(r))

				continue
			}

			word.WriteRune(r)
		}
	}

	flush()

	return tokens
}

// cloudCLIName returns the CLI the token names, and whether it names one.
func cloudCLIName(token shellToken) (string, bool) {
	if token.operator {
		return "", false
	}

	if !isCloudCLIName(token.text) {
		return "", false
	}

	return token.text, true
}

// isCloudCLIName reports whether the word is one of the three cloud CLIs' names.
func isCloudCLIName(word string) bool {
	return slices.ContainsFunc(cloudCLIs, func(cli cloudCLI) bool { return cli.name == word })
}
