package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// livePage is the live site's root page, which the rollback guard reads. A
// constant on purpose, as the endpoint's template is: no flag or variable
// points the deploy at another site.
const livePage = "https://www.shift2eu.com/"

// The environment variables sitedeploy reads, each through one os.LookupEnv.
const (
	envAccessKey   = "SCW_ACCESS_KEY"
	envSecretKey   = "SCW_SECRET_KEY" //nolint:gosec // the variable's name, not a credential
	envStepSummary = "GITHUB_STEP_SUMMARY"
)

// The timeouts: each request is bounded by requestTimeout, the whole run by
// runDeadline, inside the deploy job's timeout-minutes (PR C's workflow).
const (
	requestTimeout = 2 * time.Minute
	runDeadline    = 10 * time.Minute
)

// regionPattern is a Scaleway region (fr-par, nl-ams, pl-waw): the endpoint
// is built from it, so it is held to this shape before it reaches a URL.
var regionPattern = regexp.MustCompile(`^[a-z]{2}-[a-z]{3}$`)

func main() {
	os.Exit(run())
}

// options are the command line's flags.
type options struct {
	bundle     string
	version    string
	bucket     string
	region     string
	releases   string
	manifests  string
	listBucket bool
}

func run() int {
	signalled, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ctx, cancel := context.WithTimeout(signalled, runDeadline)
	defer cancel()

	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		return fail(err)
	}

	env, err := environment(os.LookupEnv)
	if err != nil {
		return fail(err)
	}

	releases, err := readReleases(opts.releases)
	if err != nil {
		return fail(err)
	}

	summary := io.Discard

	if env.summaryPath != "" {
		f, err := os.OpenFile(env.summaryPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return fail(fmt.Errorf("open the job summary: %w", err))
		}

		defer func() { _ = f.Close() }()

		summary = f
	}

	err = deploy.Deploy(ctx, deploy.Config{
		Endpoint:    "https://s3." + opts.region + ".scw.cloud",
		Bucket:      opts.bucket,
		Region:      opts.region,
		Credentials: env.credentials,
		Client:      &http.Client{Timeout: requestTimeout},
		Bundle:      os.DirFS(opts.bundle),
		Version:     opts.version,
		LivePage:    livePage,
		Releases:    releases,
		Manifests:   os.DirFS(opts.manifests),
		ListBucket:  opts.listBucket,
		Summary:     summary,
		Log:         os.Stdout,
		Now:         time.Now,
	})
	if err != nil {
		return fail(err)
	}

	return 0
}

// parseFlags reads the command line.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("sitedeploy", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var opts options

	flags.StringVar(&opts.bundle, "bundle", "", "the unpacked site bundle to deploy")
	flags.StringVar(&opts.version, "version", "", "the bundle's release tag (vX.Y.Z)")
	flags.StringVar(&opts.bucket, "bucket", "", "the site's bucket")
	flags.StringVar(&opts.region, "region", "", "the bucket's Object Storage region, such as fr-par")
	flags.StringVar(&opts.releases, "releases", "", "a file of the release tags on main, one per line")
	flags.StringVar(&opts.manifests, "manifests", "", "a directory of the earlier releases' manifests, <tag>.json")
	flags.BoolVar(&opts.listBucket, "list-bucket", false, "list the bucket first and fail on an object no release names")

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}

	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments %q", flags.Args())
	}

	if opts.bundle == "" || opts.version == "" || opts.bucket == "" || opts.region == "" || opts.releases == "" || opts.manifests == "" {
		return options{}, errors.New("-bundle, -version, -bucket, -region, -releases and -manifests are required")
	}

	if !regionPattern.MatchString(opts.region) {
		return options{}, errors.New("-region must be a Scaleway region such as fr-par")
	}

	return opts, nil
}

// settings is what sitedeploy reads from the step's environment.
type settings struct {
	credentials deploy.Credentials
	// summaryPath is GITHUB_STEP_SUMMARY's value, "" when it is unset.
	summaryPath string
}

// Format prints the settings with the key redacted, for every verb: fmt does
// not call Secret's own Format through an unexported field, so the settings
// print themselves.
func (s settings) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "{credentials:[redacted] summaryPath:%q}", s.summaryPath)
}

// environment reads the deploy key and the job summary's path through
// lookup (os.LookupEnv in main, the one call site-deploy-env allows; a
// function of its own in a test). Its errors name the variable, never a
// value.
func environment(lookup func(string) (string, bool)) (settings, error) {
	access, ok := lookup(envAccessKey)
	if !ok || access == "" {
		return settings{}, fmt.Errorf("%s is not set", envAccessKey)
	}

	secret, ok := lookup(envSecretKey)
	if !ok || secret == "" {
		return settings{}, fmt.Errorf("%s is not set", envSecretKey)
	}

	summaryPath, _ := lookup(envStepSummary)

	return settings{
		credentials: deploy.Credentials{AccessKey: deploy.NewSecret(access), SecretKey: deploy.NewSecret(secret)},
		summaryPath: summaryPath,
	}, nil
}

// readReleases reads the release tags, one per line; deploy.Deploy checks
// each one's shape.
func readReleases(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the workflow's own tag list
	if err != nil {
		return nil, fmt.Errorf("read the release tags: %w", err)
	}

	var tags []string

	for line := range strings.SplitSeq(string(data), "\n") {
		if tag := strings.TrimSpace(line); tag != "" {
			tags = append(tags, tag)
		}
	}

	return tags, nil
}

// fail prints err as one workflow error annotation and returns the exit code
// 1. The text is escaped as a workflow command's data, so nothing in it can
// start another command.
func fail(err error) int {
	fmt.Fprintf(os.Stderr, "::error title=Site deploy failed::%s\n", annotationData(err.Error()))

	return 1
}

// annotationData escapes s as a workflow command's data: %, CR and LF.
func annotationData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}
