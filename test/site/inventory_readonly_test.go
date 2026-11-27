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
// may run. A word that hides a command in an argument — `bash -c 'aws ...'`,
// `sh -c`, `xargs ... sh -c`, `eval "az ..."` — fails too, and so does a shell
// text the check cannot read to its end.
//
// The check is a function over text rather than over the file, so
// TestReadOnlyCheckFlagsPlantedWrites can plant a write into a copy of the guide
// in memory and see the check report it: a check that never bites would pass the
// guide today and catch nothing tomorrow.

// inventoryGuidePath is the guide this file's checks read, relative to the
// repository root.
const inventoryGuidePath = "docs/guide/inventory.md"

// azREST is `az rest`, the one command whose read depends on a flag.
const azREST = "rest"

// cloudCLIs are the three cloud CLIs the guide tells a visitor to run, each with
// the exact command paths its invocations may name and the command reference those
// paths come from, the source of every read command the list holds (issue #716's
// review: a verb rule let a write whose last word was a read verb through).
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
}, {
	name: "az",
	// The Azure CLI's reference index: `az rest` reads only with one GET method,
	// and no command that returns an account's keys or a vault's secret is a read
	// (`storage account keys list`, `keyvault secret show`).
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
// commands, and `az rest`, the one command whose read depends on a flag, must pass
// exactly one HTTP method, get.
func (c cloudCLI) reads(words []string) (string, bool) {
	commandPath := subcommandPath(words)
	held := strings.Join(commandPath, " ")

	command, ok := c.readPrefix(commandPath)
	if !ok {
		return held, false
	}

	if command != azREST {
		return held, true
	}

	methods := azHTTPMethods(words)

	switch {
	case len(methods) == 0:
		return held, false
	case len(methods) > 1:
		return held + " --method " + strings.Join(methods, " "), false
	}

	return held + " --method " + methods[0], strings.EqualFold(methods[0], "get")
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
// commands.
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

	return fmt.Sprintf("%s: %s is no read command (%q starts with none of the %d command paths cloudCLIs lists for %s; CLAUDE.md rule 1, the guide may only read): %s",
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
// shape the extraction skipped is a case that reports no write. The last case
// plants a fence label the check does not know, which must fail the reading rather
// than hide its block.
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

// TestCloudReadVerbs pins each CLI's read commands and the decision they decide.
// The lists are pinned whole, so widening one is an edit of this test too: a read
// command the guide never uses is a command no review looked at. The table below
// them pins the decision — the command path the check reports and whether it reads
// — for the shapes the lists must refuse: a positional argument that ends in a read
// word, a read that returns a secret, and `az rest` with one method, with GET, and
// with two.
func TestCloudReadVerbs(t *testing.T) {
	t.Parallel()

	lists := []struct {
		cli      string
		commands []string
	}{{
		cli: "aws",
		commands: []string{
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
	}, {
		cli: "az",
		commands: []string{
			"account show",
			"graph query",
			"rest",
			"servicebus namespace list",
			"servicebus queue list",
			"servicebus topic list",
			"servicebus topic subscription list",
			"storage account blob-service-properties show",
			"storage account list",
			"vm list-sizes",
		},
	}, {
		cli: "gcloud",
		commands: []string{
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
	}}

	for _, tc := range lists {
		t.Run("the read commands of "+tc.cli, func(t *testing.T) {
			t.Parallel()

			cli, ok := cloudCLIByName(tc.cli)
			if !ok {
				t.Fatalf("%q is no CLI this check knows, want one of the guide's: %v", tc.cli, cloudCLIs)
			}

			if diff := cmp.Diff(tc.commands, cli.readCommands); diff != "" {
				t.Errorf("%s's read commands differ from the ones the guide uses (-want +got):\n%s", tc.cli, diff)
			}
		})
	}

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
		{cli: "az", words: []string{"rest", "--method", "GET", "--url", "https://management.azure.com/x"}, verb: "rest --method GET", read: true},
		{cli: "az", words: []string{"rest", "-m", "get", "--url", "https://management.azure.com/x"}, verb: "rest --method get", read: true},
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
// write in any of those shapes reaches the check. It pins the shapes the check must
// refuse to read too: a CLI name reached through a path, a command a `bash -c`,
// `sh -c`, `xargs ... sh -c` or `eval` argument holds, and a shell text that ends
// inside a quote.
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
		want: []cloudCommand{{cli: "aws", words: []string{"opensearch", "describe-domains", "--output", "json", "--domain-names"}, where: where}},
	}, {
		name: "a command substitution inside a quoted word",
		text: `subscription="$(az account show --query id --output tsv)"`,
		want: []cloudCommand{{cli: "az", words: []string{"account", "show", "--query", "id", "--output", "tsv"}, where: where}},
	}, {
		name: "a command in a backtick substitution inside a quoted word",
		text: "volumes=\"`aws ec2 create-tags --resources i-0123456789abcdef0`\"",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where}},
	}, {
		name: "a command in a backtick substitution outside quotes",
		text: "out=`aws ec2 create-tags --resources i-0123456789abcdef0`",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where}},
	}, {
		name: "a command reached through its path",
		text: "/usr/bin/aws ec2 create-tags --resources i-0123456789abcdef0",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where}},
	}, {
		name:        "a command a bash -c argument holds",
		text:        `bash -c 'aws ec2 create-tags --resources i-0123456789abcdef0'`,
		wantProblem: "holds a aws command",
	}, {
		name:        "a command an xargs sh -c argument holds",
		text:        `xargs -r -n 1 sh -c 'gcloud compute instances delete shop-web --quiet'`,
		wantProblem: "holds a gcloud command",
	}, {
		name:        "a command an eval argument holds",
		text:        `eval "az group delete --name shop-web --yes"`,
		wantProblem: "holds a az command",
	}, {
		name: "a heredoc body that is no command",
		text: "cat > \"$out/notes.txt\" <<'EOF'\n" +
			"the zone's name is the visitor domain\n" +
			"EOF\n" +
			"aws ec2 create-tags --resources i-0123456789abcdef0\n",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: where}},
	}, {
		name:        "a shell text that ends inside a quote",
		text:        "aws ec2 describe-volumes --query 'Volumes\n",
		wantProblem: "ends inside a single quote",
	}, {
		name:        "a shell text that ends inside a command substitution",
		text:        `out="$(aws ec2 describe-volumes`,
		wantProblem: "ends inside a command substitution",
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

			got, problems := invocationsIn(tc.text, true, where)

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

// TestProgramCommentsFindEveryShape pins what the comments of a program the guide
// ships are read for (issue #716): a CLI name anywhere in a comment line starts an
// invocation — `# then run: aws ec2 create-tags` is the write a line-start rule
// would miss — while a CLI name that follows a word is the comment's prose, and a
// command a program wraps across two lines is read whole. The wanted writes are
// what the check refuses: a planted write in a comment must be one of them.
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
	}{{
		name: "a comment that starts with a command",
		text: "# aws ec2 describe-instances --output json\n",
		want: []cloudCommand{{cli: "aws", words: []string{"ec2", "describe-instances", "--output", "json"}, where: program + ", line 1"}},
	}, {
		name:       "a write a comment names after its prose",
		text:       "# then run: aws ec2 create-tags --resources i-0123456789abcdef0\n",
		want:       []cloudCommand{{cli: "aws", words: []string{"ec2", "create-tags", "--resources", "i-0123456789abcdef0"}, where: program + ", line 1"}},
		wantWrites: []string{"ec2 create-tags"},
	}, {
		name: "a command a comment wraps across two lines",
		text: "#   {\"data\": ...}   az servicebus topic\n#                    subscription list\n",
		want: []cloudCommand{{cli: "az", words: []string{"servicebus", "topic", "subscription", "list"}, where: program + ", line 1"}},
	}, {
		name: "a command a comment follows with a comma and its prose",
		text: "# The listing (az graph query, one page) is the list of resources\n",
		want: []cloudCommand{{cli: "az", words: []string{"graph", "query"}, where: program + ", line 1"}},
	}, {
		name: "prose that names a CLI and no command",
		text: "# as gcloud writes a string), and never a command\n",
	}, {
		name: "a shape line that names an operation without the CLI",
		text: "#   {\"Buckets\": ...}   s3api list-buckets\n",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := scriptInvocations(program, tc.text)

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(cloudCommand{})); diff != "" {
				t.Errorf("the invocations of the comments %q differ from the ones they hold (-want +got):\n%s", tc.text, diff)
			}

			var writes []string

			for _, w := range notReadOnly(got) {
				writes = append(writes, w.path)
			}

			if diff := cmp.Diff(tc.wantWrites, writes); diff != "" {
				t.Errorf("the read-only check over the comments %q refuses %v, want %v: a program the guide ships may only read either",
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

// azHTTPMethods returns every HTTP method an az invocation passes, as `--method x`,
// `-m x` and `--method=x` write it, in the order it passes them. Every occurrence
// counts: `az rest` sends one request, so a second method would be a second
// request, and a write in a read's clothes.
func azHTTPMethods(words []string) []string {
	var methods []string

	for i := 0; i < len(words); i++ {
		if method, ok := strings.CutPrefix(words[i], "--method="); ok {
			methods = append(methods, method)

			continue
		}

		if (words[i] == "--method" || words[i] == "-m") && i+1 < len(words) {
			methods = append(methods, words[i+1])

			i++
		}
	}

	return methods
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

		programs = append(programs, shippedProgram{
			path:     programPath,
			commands: scriptInvocations(programPath, string(readRepositoryFile(t, programPath))),
		})
	}

	return programs
}

// guideTextFindings returns the cloud CLI invocations a guide text holds — in its
// shell blocks, in the inline code spans of its prose and in the prose lines that
// start with a CLI name — and the problems that keep the check from reading it: a
// fence label that is neither a shell language nor on the closed list of prose
// languages, a fence that never closes, and a shell text it cannot read to its end
// or that hides a command in a word. A problem is a failure: a text the check could
// not read is no text it may pass.
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

		if marker, info, opens := fenceOpen(trimmed); opens {
			body, last, closed := fenceBody(lines, i, marker)
			if !closed {
				problems = append(problems, fmt.Sprintf("%s, the fence that opens at line %d: it never closes, so the check cannot see where what it holds ends",
					guideLocation(section), i+1))
			}

			i = last

			shell, err := fenceLanguage(leadingWord(info))

			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("%s, the fence that opens at line %d: %v", guideLocation(section), i+1, err))
			case shell:
				found, refused := invocationsIn(strings.Join(body, "\n"), true, guideLocation(section))
				commands = append(commands, found...)
				problems = append(problems, refused...)
			}

			continue
		}

		where := guideLocation(section)

		for _, span := range codeSpans(lines[i]) {
			found, refused := invocationsIn(span, true, where)
			commands = append(commands, found...)
			problems = append(problems, refused...)
		}

		if startsWithCloudCLI(trimmed) {
			found, refused := invocationsIn(trimmed, true, where)
			commands = append(commands, found...)
			problems = append(problems, refused...)
		}
	}

	return commands, problems
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

// fenceOpen reports whether a trimmed line opens a fenced block, and returns the
// fence's marker — the character it is made of, repeated its length — and its info
// string, so a `~~~bash title="x"` fence is as much a fence as a ```bash one.
func fenceOpen(line string) (marker, info string, ok bool) {
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

	return strings.Repeat(line[:1], length), strings.TrimSpace(line[length:]), true
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
// argument is one word however many lines it spans.
//
// It reports the texts the check may not pass: one it cannot read to its end — an
// unbalanced quote or substitution, a heredoc with no delimiter line — and a word
// that holds a whole command, as an argument to `bash -c`, `sh -c`, `xargs ... sh
// -c` or `eval` does, which runs for the visitor as surely as a command of its own.
func invocationsIn(text string, newlineEndsCommand bool, where string) ([]cloudCommand, []string) {
	tokens, err := shellTokens(text, newlineEndsCommand)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", where, err)}
	}

	var (
		commands []cloudCommand
		problems []string
	)

	for i, token := range tokens {
		if token.operator {
			continue
		}

		cli, invocation := cloudCLIName(token)
		if !invocation {
			if name, hidden := hiddenCLI(token.text); hidden {
				problems = append(problems, fmt.Sprintf("%s: the word %q is no invocation this check can read, and it holds a %s command: an argument to `bash -c`, `sh -c` or `eval` runs for the visitor too, so the check cannot pass it",
					where, token.text, name))
			}

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
			continue
		}

		commands = append(commands, cloudCommand{cli: cli, words: words, where: where})
	}

	return commands, problems
}

// hiddenCLI returns the CLI a word that is no invocation holds, and whether it
// holds one: `aws `, `az ` or `gcloud ` followed by a letter, at a word boundary,
// as a quoted argument to `bash -c` writes it.
func hiddenCLI(word string) (string, bool) {
	for _, cli := range cloudCLIs {
		for at := 0; at+len(cli.name) < len(word); at++ {
			if word[at:at+len(cli.name)] != cli.name {
				continue
			}

			if at > 0 && isWordByte(word[at-1]) {
				continue
			}

			if word[at+len(cli.name)] != ' ' {
				continue
			}

			next := at + len(cli.name) + 1
			if next < len(word) && isLetter(word[next]) {
				return cli.name, true
			}
		}
	}

	return "", false
}

// isLetter reports whether the byte is an ASCII letter.
func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isWordByte reports whether the byte can be part of a shell word: a letter, a
// digit, or one of the characters a program's name and arguments are made of.
func isWordByte(b byte) bool {
	return isLetter(b) || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' || b == '/'
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

			name := leadingWord(command)

			open = &cloudCommand{
				cli:   name,
				words: wordTokens(strings.TrimSpace(strings.TrimPrefix(command, name))),
				where: fmt.Sprintf("%s, line %d", programPath, number+1),
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

// shellToken is one token of a shell text: a word, or the operator that ends one.
type shellToken struct {
	text     string
	operator bool
}

// The shell contexts a word can be inside, besides a quote: a command substitution
// (`$(` or a backtick) inside a double-quoted string, in which the words split as
// they do outside quotes, and the character that closes it returns the scan to the
// quotes.
const (
	substitution         = 's'
	backtickSubstitution = '`'
)

// shellTokens splits a shell text into the words a program is given and the
// operators that end them: a quote keeps its text, newlines and spaces inside it
// included, in one word; a line continuation joins two lines into one; a comment
// runs to the end of its line; white space, a newline, a pipe, a redirection and a
// parenthesis end a word; a backtick is an operator outside quotes and a
// substitution inside them; and a `<<` heredoc's body is no command's text, so it
// is skipped to its delimiter line. newlineEndsCommand says whether an unquoted
// newline ends an invocation, as it does in a command block and does not in a
// comment whose command a program wraps across two lines.
//
// It returns an error, and no tokens, for a text it cannot read to the end: one
// that ends inside a quote or a substitution, and one whose heredoc has no
// delimiter line. A quote whose end the check never saw would swallow every command
// after it, so the text is no text it may read.
func shellTokens(text string, newlineEndsCommand bool) ([]shellToken, error) {
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
			case r == '`':
				// A backtick substitutes a command too.
				flush()
				contexts = append(contexts, backtickSubstitution)
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
		case r == '`':
			if context() == backtickSubstitution {
				contexts = contexts[:len(contexts)-1]

				continue
			}

			operator("`")
		case r == ')' && context() == substitution:
			contexts = contexts[:len(contexts)-1]
			operator(")")
		case r == '<' && i+1 < len(runes) && runes[i+1] == '<' && (i+2 >= len(runes) || runes[i+2] != '<'):
			operator("<<")

			last, err := heredocEnd(runes, i)
			if err != nil {
				return nil, err
			}

			i = last
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
			if r == '|' || r == '&' || r == ';' || r == '(' || r == ')' || r == '<' || r == '>' {
				operator(string(r))

				continue
			}

			word.WriteRune(r)
		}
	}

	if len(contexts) > 0 {
		return nil, fmt.Errorf("the text ends inside %s, so the check cannot read the commands after it", contextName(contexts[len(contexts)-1]))
	}

	flush()

	return tokens, nil
}

// heredocEnd returns the index of the last rune of the line that ends the body of
// the `<<` heredoc that starts at the rune index, and whether the body ends: a
// heredoc's body is data, not commands, so the scan skips it, an apostrophe in it
// included.
func heredocEnd(runes []rune, at int) (int, error) {
	i := at + 2

	stripTabs := false

	if i < len(runes) && runes[i] == '-' {
		stripTabs = true
		i++
	}

	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}

	var (
		delimiter strings.Builder
		quote     rune
	)

	for ; i < len(runes); i++ {
		r := runes[i]

		if quote != 0 {
			if r == quote {
				quote = 0

				continue
			}

			delimiter.WriteRune(r)

			continue
		}

		if r == '\'' || r == '"' {
			quote = r

			continue
		}

		if r == ' ' || r == '\t' || r == '\n' {
			break
		}

		delimiter.WriteRune(r)
	}

	want := delimiter.String()
	if want == "" {
		return 0, fmt.Errorf("a `<<` heredoc names no delimiter, so the check cannot see where its body stops")
	}

	line := i
	for line < len(runes) && runes[line] != '\n' {
		line++
	}

	if line >= len(runes) {
		return 0, fmt.Errorf("the heredoc `<<%s` begins on a text with no body, so the check cannot see where it stops", want)
	}

	for line++; line < len(runes); {
		end := line
		for end < len(runes) && runes[end] != '\n' {
			end++
		}

		body := strings.TrimRight(string(runes[line:end]), "\r")
		if stripTabs {
			body = strings.TrimLeft(body, "\t")
		}

		if body == want {
			return end, nil
		}

		line = end + 1
	}

	return 0, fmt.Errorf("the heredoc `<<%s` has no line that ends it, so the check cannot see where its body stops", want)
}

// contextName names the shell context a scan ended in, for a failure message.
func contextName(context rune) string {
	switch context {
	case '\'':
		return "a single quote"
	case '"':
		return "a double quote"
	case substitution:
		return "a command substitution"
	case backtickSubstitution:
		return "a backtick substitution"
	}

	return "a quote"
}

// cloudCLIName returns the CLI the token names, and whether it names one: its last
// path element, so `/usr/bin/aws` is aws and `aws.jq` is no invocation.
func cloudCLIName(token shellToken) (string, bool) {
	if token.operator {
		return "", false
	}

	name := path.Base(token.text)
	if !isCloudCLIName(name) {
		return "", false
	}

	return name, true
}

// isCloudCLIName reports whether the word is one of the three cloud CLIs' names.
func isCloudCLIName(word string) bool {
	return slices.ContainsFunc(cloudCLIs, func(cli cloudCLI) bool { return cli.name == word })
}
