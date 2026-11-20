package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// The binary the release builds, and the name_template its archive must have:
// the CLI's is goreleaser's default written out, so its archives keep the name
// shift2eu_<version>_<os>_<arch> they have always had (issue #147).
const (
	cliBinary          = "shift2eu"
	cliArchiveName     = "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
	checksumsFile      = "checksums.txt"
	archiveFormat      = "tar.gz"
	windowsFormat      = "zip"
	archiveCheckStep   = "Each archive holds its own binary"
	archiveCheckScript = ".github/scripts/verify-archives.sh"
	archiveCheckJob    = "test"

	// goreleaserActionName is the action that installs GoReleaser in the dry
	// run and runs it in releaseGoReleaserJob, the release.yml job that
	// publishes the archives.
	goreleaserActionName = "goreleaser/goreleaser-action"
	releaseGoReleaserJob = "goreleaser"
)

// retiredNames are the names no release document may hold any more: the HTTP
// API's binary and the bundled OpenAPI document that shipped beside the
// archives. The release publishes the CLI alone (issue #393), so
// .goreleaser.yaml and release.yml are refused if a line of theirs, a comment
// included, still names one of them. "openapi" covers both the bundler's path
// (api/openapi.bundled.json) and the asset name it used to be published as
// (shift2eu-server_openapi.json).
var retiredNames = []string{"shift2eu-server", "openapi"}

// retiredNameMessages returns a message for every retired name line holds: a
// release document may not mention the HTTP API's binary or its OpenAPI
// document in any way, published or commented out (issue #393).
func retiredNameMessages(line string) []string {
	var messages []string

	for _, name := range retiredNames {
		if strings.Contains(line, name) {
			messages = append(messages, fmt.Sprintf("the line %q names %s, which the release no longer publishes: it ships the CLI alone (issue #393)", line, name))
		}
	}

	return messages
}

// retiredNameMessagesIn returns a message for every line of doc that names a
// retired name, each as retiredNameMessages words it.
func retiredNameMessagesIn(doc []byte) []string {
	var messages []string

	for _, line := range strings.Split(string(doc), "\n") {
		messages = append(messages, retiredNameMessages(strings.TrimSpace(strings.TrimSuffix(line, "\r")))...)
	}

	return messages
}

// goreleaserSettings are the with: keys of goreleaser-action compared between
// the dry run and the release: which GoReleaser each installs.
var goreleaserSettings = []string{"distribution", "version"}

// archiveNames are the name_template of the one binary's archive, by binary.
var archiveNames = map[string]string{
	cliBinary: cliArchiveName,
}

// archiveKeys are the keys an archive entry may hold: the section is a closed
// schema, so a key the check never lists cannot change what an archive is.
var archiveKeys = []string{"id", "ids", "name_template", "formats", "format_overrides", "files"}

// archiveFiles are the files every archive ships beside its binary: LICENSE,
// README.md, the pages under docs/ that README.md links to (issue #149) and
// the jq programs docs/guide/inventory.md builds an inventory with (issue
// #178), and the sample inventories under examples/ that docs/guide/try-it.md
// runs the CLI on (issue #188).
var archiveFiles = []string{"LICENSE", "README.md", "docs/*.md", "docs/**/*", "examples/*.md", "examples/**/*"}

// GoReleaserArchives returns one message for every way the .goreleaser.yaml
// document goreleaser lets one archive hold more than the CLI's binary, or
// moves that binary out of the archive its downloads expect (issue #147); none
// for a correct document:
//
//   - no line, comments included, names shift2eu-server or openapi: the
//     release publishes the CLI alone, so the HTTP API's build, its archive
//     and its OpenAPI document are gone rather than merely unreferenced (issue
//     #393);
//   - archives: holds exactly one entry, naming exactly one build through ids:
//     (an entry without ids takes every build), and every build of builds:
//     sits in that archive;
//   - the entry's id: is the one build its ids: names, so the CLI's;
//   - its name_template is the one of its build's binary: goreleaser's default
//     written out for the CLI (shift2eu);
//   - the entry is tar.gz, and zip on Windows, and ships exactly LICENSE,
//     README.md, docs/*.md, docs/**/*, examples/*.md and examples/**/*;
//   - an entry holds no key beyond id, ids, name_template, formats,
//     format_overrides and files;
//   - checksum: holds exactly name_template: checksums.txt, one file covering
//     the archive, and no extra_files: nothing is published beside the
//     archives (issue #393).
func GoReleaserArchives(goreleaser []byte) []string {
	r := &schemaReader{}

	lines, err := yamlread.SplitLines(goreleaser)
	if err != nil {
		return []string{fmt.Sprintf("read .goreleaser.yaml: %v", err)}
	}

	top, err := yamlread.Mapping(lines)
	if err != nil {
		return []string{fmt.Sprintf("read .goreleaser.yaml: %v", err)}
	}

	binaries := r.buildBinaries(top)
	if binaries == nil {
		return r.messages
	}

	r.archives(top, binaries)
	r.checksum(top)

	return append(r.messages, retiredNameMessagesIn(goreleaser)...)
}

// buildBinaries returns the binary: of each build of builds:, by its id; nil
// when it cannot read them, with the reason in the messages.
func (r *schemaReader) buildBinaries(top []yamlread.Entry) map[string]string {
	builds, ok := child(top, "builds")
	if !ok {
		r.fail("read .goreleaser.yaml: no builds section, want the builds the archives hold")

		return nil
	}

	items, err := yamlread.Sequence(r.block(builds, "builds"))
	if err != nil {
		r.fail("read .goreleaser.yaml: builds: %v", err)

		return nil
	}

	binaries := map[string]string{}

	for _, it := range items {
		entries, err := yamlread.Mapping(it.Lines)
		if err != nil {
			r.fail("read .goreleaser.yaml: builds: line %d: %v", it.Num, err)

			return nil
		}

		id, okID := child(entries, "id")
		binary, okBinary := child(entries, "binary")

		if !okID || !okBinary {
			r.fail("line %d: a build without id: and binary:, want both, so an archive's ids names the binary it holds", it.Num)

			return nil
		}

		idValue, errID := yamlread.Single(id)
		binaryValue, errBinary := yamlread.Single(binary)

		if errID != nil || errBinary != nil {
			r.fail("line %d: a build whose id: or binary: is not one scalar: %v", it.Num, firstError(errID, errBinary))

			return nil
		}

		if _, repeated := binaries[idValue]; repeated {
			r.fail("line %d: build id %s is repeated", id.Num, idValue)

			return nil
		}

		binaries[idValue] = binaryValue
	}

	return binaries
}

// firstError returns the first error of errs that is not nil.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

// archives checks the archives: section against the builds, binaries by id.
func (r *schemaReader) archives(top []yamlread.Entry, binaries map[string]string) {
	section, ok := child(top, "archives")
	if !ok || (section.Value == "" && len(section.Block) == 0) {
		r.fail("no archives section, want one entry, one per build (%s): goreleaser's default archive takes every build", cliBinary)

		return
	}

	block := r.block(section, "archives")
	if block == nil {
		return
	}

	items, err := yamlread.Sequence(block)
	if err != nil {
		r.fail("archives: %v", err)

		return
	}

	if len(items) != len(archiveNames) {
		r.fail("line %d: archives holds %d entries, want exactly %d, the CLI's build (%s)", section.Num, len(items), len(archiveNames), cliBinary)
	}

	// archived counts the archive entries that name each build, and ids the
	// archive entries that carry each id.
	archived := map[string]int{}
	ids := map[string]int{}

	for _, it := range items {
		r.archive(it, binaries, archived, ids)
	}

	for _, id := range sortedKeys(binaries) {
		switch n := archived[id]; n {
		case 1:
		case 0:
			r.fail("build %s (binary %s) is in no archive: want exactly one archive entry whose ids names it alone", id, binaries[id])
		default:
			r.fail("build %s (binary %s) is in %d archives: want exactly one archive entry whose ids names it alone", id, binaries[id], n)
		}
	}
}

// archive checks one archive entry, counts the builds its ids names in
// archived and its id in ids.
func (r *schemaReader) archive(it yamlread.Item, binaries map[string]string, archived, ids map[string]int) {
	path := fmt.Sprintf("archives: entry at line %d", it.Num)

	entries, err := yamlread.Mapping(it.Lines)
	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	for _, e := range entries {
		if !slices.Contains(archiveKeys, e.Key) {
			r.fail("line %d: %s holds the key %s, want only %s: a key the check never lists may change what the archive is", e.Num, path, e.Key, strings.Join(archiveKeys, ", "))
		}
	}

	build := r.archiveBuild(entries, path, binaries, archived)
	r.archiveID(entries, path, build, ids)

	r.archiveName(entries, path, binaries[build])
	r.archiveFormats(entries, path)
	r.archiveFiles(entries, path)
}

// archiveBuild checks the ids of an archive entry, counts the builds it names
// in archived, and returns the id of the one build it names; "" when it does
// not name exactly one build.
func (r *schemaReader) archiveBuild(entries []yamlread.Entry, path string, binaries map[string]string, archived map[string]int) string {
	e, ok := child(entries, "ids")
	if !ok {
		r.fail("%s holds no ids, want ids naming exactly one build: an entry without ids takes every build, so its download would hold both binaries", path)

		for id := range binaries {
			archived[id]++
		}

		return ""
	}

	ids, err := yamlread.List(e)
	if err != nil {
		r.fail("%s: ids: %v", path, err)

		return ""
	}

	for _, id := range ids {
		if _, known := binaries[id]; !known {
			r.fail("line %d: %s: ids names %s, which no build of builds: has", e.Num, path, id)

			continue
		}

		archived[id]++
	}

	if len(ids) != 1 {
		r.fail("line %d: %s: ids is %q, want exactly one build, so the archive holds one binary", e.Num, path, ids)

		return ""
	}

	if _, known := binaries[ids[0]]; !known {
		return ""
	}

	return ids[0]
}

// archiveID checks that the id: of an archive entry is build, the one build
// its ids: names, and that no earlier entry carries it, counting it in ids;
// build is "" when the entry names no single build.
func (r *schemaReader) archiveID(entries []yamlread.Entry, path, build string, ids map[string]int) {
	e, ok := child(entries, "id")
	if !ok {
		want := build
		if want == "" {
			want = "the one build its ids names"
		}

		r.fail("%s holds no id, want id: %s: goreleaser names an entry without id \"default\", so two such entries are one archive to it and goreleaser check refuses the document", path, want)

		return
	}

	got, err := yamlread.Single(e)
	if err != nil {
		r.fail("%s: id: %v", path, err)

		return
	}

	ids[got]++
	if ids[got] == 2 {
		r.fail("line %d: %s: id %s is repeated, want each archive entry to carry its own id, the one build its ids names", e.Num, path, got)
	}

	if build != "" && got != build {
		r.fail("line %d: %s: id is %s, want %s, the one build its ids names", e.Num, path, got, build)
	}
}

// archiveName checks the name_template of an archive entry against the one
// of binary, the binary its build makes; binary is "" when the entry names no
// single build.
func (r *schemaReader) archiveName(entries []yamlread.Entry, path, binary string) {
	e, ok := child(entries, "name_template")
	if !ok {
		r.fail("%s holds no name_template, want the one of its binary: %q for %s", path, cliArchiveName, cliBinary)

		return
	}

	if binary == "" {
		return
	}

	want, known := archiveNames[binary]
	if !known {
		r.fail("line %d: %s holds the binary %s, want %s", e.Num, path, binary, cliBinary)

		return
	}

	got, err := yamlread.Single(e)
	if err != nil {
		r.fail("%s: name_template: %v", path, err)

		return
	}

	if got != want {
		r.fail("line %d: %s: name_template is %q, want exactly %q, the name the downloads of %s expect", e.Num, path, got, want, binary)
	}
}

// archiveFormats checks that an archive entry is tar.gz on every platform
// and zip on Windows.
func (r *schemaReader) archiveFormats(entries []yamlread.Entry, path string) {
	if e, ok := child(entries, "formats"); ok {
		r.list(e, path+": formats", []string{archiveFormat})
	} else {
		r.fail("%s holds no formats, want [%s]", path, archiveFormat)
	}

	e, ok := child(entries, "format_overrides")
	if !ok {
		r.fail("%s holds no format_overrides, want goos: windows with formats: [%s]", path, windowsFormat)

		return
	}

	overridesPath := path + ": format_overrides"

	block := r.block(e, overridesPath)
	if block == nil {
		return
	}

	items, err := yamlread.Sequence(block)
	if err != nil {
		r.fail("%s: %v", overridesPath, err)

		return
	}

	if len(items) != 1 {
		r.fail("line %d: %s holds %d entries, want exactly one: goos: windows with formats: [%s]", e.Num, overridesPath, len(items), windowsFormat)

		return
	}

	override := r.exactKeys(items[0].Lines, overridesPath, "goos", "formats")
	if override == nil {
		return
	}

	r.scalar(override["goos"], overridesPath+": goos", exactly("windows"))
	r.list(override["formats"], overridesPath+": formats (Windows, want zip)", []string{windowsFormat})
}

// archiveFiles checks that an archive entry ships exactly archiveFiles: beside
// its binary, LICENSE, README.md and the pages under docs/ (issue #149), so
// README.md's links work in an unpacked archive, the jq programs
// docs/guide/inventory.md builds an inventory with (issue #178), and the
// sample inventories under examples/ that docs/guide/try-it.md runs the CLI on
// (issue #188).
func (r *schemaReader) archiveFiles(entries []yamlread.Entry, path string) {
	e, ok := child(entries, "files")
	if !ok {
		r.fail("%s holds no files, want exactly %s", path, strings.Join(archiveFiles, ", "))

		return
	}

	got, err := yamlread.List(e)
	if err != nil {
		r.fail("%s: files: %v", path, err)

		return
	}

	for _, want := range archiveFiles {
		if !slices.Contains(got, want) {
			r.fail("line %d: %s ships no %s, want exactly %s: README.md links to LICENSE and the pages under docs/ (issue #149), docs/guide/inventory.md builds an inventory with the jq programs under docs/guide/inventory/ (issue #178), and docs/guide/try-it.md runs the CLI on the sample inventories under examples/ (issue #188)", e.Num, path, want, strings.Join(archiveFiles, ", "))
		}
	}

	for i, file := range got {
		if !slices.Contains(archiveFiles, file) || slices.Index(got, file) != i {
			r.fail("line %d: %s ships %s, which the check never lists or lists once: want exactly %s", e.Num, path, file, strings.Join(archiveFiles, ", "))
		}
	}
}

// checksum checks that the checksum section holds exactly
// name_template: checksums.txt, one file covering the archive, and nothing
// else: the release publishes no extra file beside the archives (issue #393).
func (r *schemaReader) checksum(top []yamlread.Entry) {
	section, ok := child(top, "checksum")
	if !ok {
		r.fail("no checksum section, want checksum: name_template: %s, one file covering the archive", checksumsFile)

		return
	}

	block := r.block(section, "checksum")
	if block == nil {
		return
	}

	entries, err := yamlread.Mapping(block)
	if err != nil {
		r.fail("checksum: %v", err)

		return
	}

	for _, e := range entries {
		if e.Key != "name_template" {
			r.fail("line %d: checksum holds the key %s, want only name_template: %s, one file covering the archive: the release publishes no extra file beside the archives (issue #393)", e.Num, e.Key, checksumsFile)
		}
	}

	e, ok := child(entries, "name_template")
	if !ok {
		r.fail("line %d: checksum holds no name_template, want name_template: %s", section.Num, checksumsFile)

		return
	}

	r.scalar(e, "checksum: name_template ("+checksumsFile+")", exactly(checksumsFile))
}

// DryRunVerifiesArchives returns a message unless the test job of the ci.yml
// document ci, jobs: test, which runs on every pull request and every push to
// main, holds exactly one step named "Each archive holds its own binary"
// whose run: is exactly .github/scripts/verify-archives.sh, and no other job
// holds a step of that name (issue #147). The script builds the release's
// archives with a goreleaser snapshot and fails unless each holds exactly its
// own binary.
//
// The snapshot must be the GoReleaser that publishes the archives, so that a
// Renovate pull request bumping one workflow cannot let the dry run check them
// with another: the test job also holds exactly one goreleaser-action step,
// before the archive check's, whose uses: (the full commit), with:
// distribution and with: version are those of the goreleaser-action step of
// the release.yml document release's goreleaser job, and whose with:
// install-only is true, so the step installs GoReleaser and publishes nothing.
func DryRunVerifiesArchives(ci, release []byte) []string {
	r := &schemaReader{raw: rawLines(ci)}

	lines, err := yamlread.SplitLines(ci)
	if err != nil {
		return []string{fmt.Sprintf("read ci.yml: %v", err)}
	}

	jobsBlock, err := yamlread.Walk(lines, "jobs")
	if err != nil {
		return []string{fmt.Sprintf("read ci.yml: %v", err)}
	}

	jobs, err := yamlread.Mapping(jobsBlock)
	if err != nil {
		return []string{fmt.Sprintf("read ci.yml: jobs: %v", err)}
	}

	var found []yamlread.Entry

	for _, job := range jobs {
		for _, step := range r.namedSteps(job, archiveCheckStep) {
			if job.Key != archiveCheckJob {
				r.fail("line %d: jobs: %s holds the step %q, want it in jobs: %s, which runs on every pull request and every push to main", step.Num, job.Key, archiveCheckStep, archiveCheckJob)

				continue
			}

			found = append(found, step)
		}
	}

	switch len(found) {
	case 0:
		r.fail("jobs: %s holds no step named %q running %s: want it, so every pull request builds the release's archives and checks each holds exactly its own binary", archiveCheckJob, archiveCheckStep, archiveCheckScript)
	case 1:
		path := fmt.Sprintf("jobs: %s: step %q (line %d)", archiveCheckJob, archiveCheckStep, found[0].Num)

		step := r.exactKeys(found[0].Block, path, "name", "run")
		if step != nil {
			r.run(step["run"], path+": run", archiveCheckScript)
		}

		r.dryRunGoReleaser(ci, release, found[0].Num)
	default:
		r.fail("jobs: %s holds %d steps named %q, want exactly one running %s", archiveCheckJob, len(found), archiveCheckStep, archiveCheckScript)
	}

	return r.messages
}

// dryRunGoReleaser checks the goreleaser-action step of the ci.yml document
// ci's test job against the one of the release.yml document release's
// goreleaser job: one step, before the archive check's step at line
// checkLine, at the release's commit, distribution and version, install-only.
func (r *schemaReader) dryRunGoReleaser(ci, release []byte, checkLine int) {
	ciJob, ok := r.readJob(ci, "ci.yml", archiveCheckJob)
	if !ok {
		return
	}

	releaseJob, ok := r.readJob(release, "release.yml", releaseGoReleaserJob)
	if !ok {
		return
	}

	d := dryRun{r: r, ciRaw: rawLines(ci), releaseRaw: rawLines(release)}

	installs := stepsOf(ciJob.steps, goreleaserActionName)
	if len(installs) != 1 {
		r.fail("ci.yml: jobs: %s holds %d %s steps, want exactly 1, before the step %q: it installs the GoReleaser release.yml runs, so the snapshot builds the archives the release publishes", archiveCheckJob, len(installs), goreleaserActionName, archiveCheckStep)

		return
	}

	install := installs[0]

	if install.num > checkLine {
		r.fail("ci.yml: jobs: %s: the %s step (line %d) comes after the step %q (line %d), want it before: the archive check runs the GoReleaser it installs", archiveCheckJob, goreleaserActionName, install.num, archiveCheckStep, checkLine)
	}

	releaseStep, ok := d.one(releaseJob.steps, "release.yml's "+releaseGoReleaserJob+" job", goreleaserActionName)
	if !ok {
		return
	}

	if install.commit != releaseStep.commit {
		r.fail("ci.yml: %s (line %d): uses is %s@%s, release.yml's (line %d) %s@%s: the dry run must build the archives with the GoReleaser the release runs", goreleaserActionName, install.num, goreleaserActionName, install.commit, releaseStep.num, goreleaserActionName, releaseStep.commit)
	}

	for _, key := range goreleaserSettings {
		d.same(install, releaseStep, goreleaserActionName, key)
	}

	if got, ok := d.withValue(d.ciRaw, install, "ci.yml: "+goreleaserActionName, "install-only"); ok && got != "true" {
		r.fail("ci.yml: %s (line %d): with: install-only is %q, want true: without it the action runs goreleaser release itself", goreleaserActionName, install.num, got)
	}
}

// namedSteps returns the steps of job named name, each as an Entry whose Num
// is the step's line and whose Block is its lines.
func (r *schemaReader) namedSteps(job yamlread.Entry, name string) []yamlread.Entry {
	path := "jobs: " + job.Key

	keys, err := yamlread.Mapping(job.Block)
	if err != nil {
		r.fail("%s: %v", path, err)

		return nil
	}

	steps, ok := child(keys, "steps")
	if !ok {
		return nil
	}

	items, err := yamlread.Sequence(steps.Block)
	if err != nil {
		r.fail("%s: steps: %v", path, err)

		return nil
	}

	var out []yamlread.Entry

	for _, it := range items {
		entries, err := yamlread.Mapping(it.Lines)
		if err != nil {
			r.fail("%s: step at line %d: %v", path, it.Num, err)

			continue
		}

		e, ok := child(entries, "name")
		if !ok {
			continue
		}

		got, err := yamlread.Single(e)
		if err != nil || got != name {
			continue
		}

		out = append(out, yamlread.Entry{Num: it.Num, Block: it.Lines})
	}

	return out
}
