package publish

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Tool is one tool the run used, and its version.
type Tool struct {
	Name    string
	Version string
}

// Check is one check of the coverage table: whether it ran, and the why id
// when it did not.
type Check struct {
	ID     string
	Ran    bool
	NotRun string
}

// Finding is one finding of the model's result. Every field is model text:
// the core reads Check, Severity, Confirmed, Category and Private to place it,
// Rule, File and Sink to fingerprint it, and renders the rest only escaped.
type Finding struct {
	Check     string
	Rule      string
	File      string
	Line      int
	Sink      string
	Severity  string
	Confirmed bool
	Category  string
	// Private is the model's flag, the schema's private: it can only make
	// the finding more private.
	Private  bool
	Scenario string
	// Evidence is the PoC or trace, for a draft advisory only: no report and
	// no public issue holds it, since the encoded-run refusal never reads it.
	Evidence  string
	Fix       string
	Dismissal string
}

// Changes is the model's account of what changed since the last report.
type Changes struct {
	Dependencies []string
	Narrative    string
}

// Result is the model's structured result, as ParseResult accepted it.
type Result struct {
	Schema      int
	Commit      string
	ReleaseTag  string
	ImageDigest string
	Tools       []Tool
	Checks      []Check
	Findings    []Finding
	Changes     Changes
	Limits      []string
}

// The schema's caps, in bytes.
const (
	maxTools         = 16
	maxDependencies  = 50
	maxLimits        = 40
	maxFileBytes     = 200
	maxLine          = 9999999
	maxSeverityBytes = 16
	maxCategoryBytes = 32
	maxScenarioBytes = 2000
	maxEvidenceBytes = 16000
	maxFixBytes      = 1000
	maxDismissal     = 200
	maxDependency    = 160
	maxNarrative     = 2000
	maxLimitBytes    = 300
)

var (
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	tagPattern     = regexp.MustCompile(`^v(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})$`)
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^v?[0-9]{1,4}(\.[0-9]{1,4}){0,3}$`)
	rulePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,79}$`)
	filePattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+@:/-]*$`)
	sinkPattern    = regexp.MustCompile(`^[A-Za-z0-9_.()*/:\[\]-]{1,120}$`)
	integerLiteral = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,15})$`)
)

// ParseResult reads the model's result.json and accepts it only if every rule
// of ADR 0040's result schema holds and its commit is commit, the job's
// github.sha. Any refusal wraps ErrResultInvalid and names the field's path
// and the rule, never the value; a commit argument that is no commit wraps
// ErrConfig.
func ParseResult(data []byte, commit string) (Result, error) {
	if !commitPattern.MatchString(commit) {
		return Result{}, fmt.Errorf("commit argument: not 40 lowercase hex characters: %w", ErrConfig)
	}

	if len(data) > MaxResultBytes {
		return Result{}, invalid("result", "larger than MaxResultBytes")
	}

	tree, err := parseJSON(data)
	if err != nil {
		return Result{}, invalid("result", err.Error())
	}

	result, err := decodeResult(tree)
	if err != nil {
		return Result{}, err
	}

	if err := validateResult(result); err != nil {
		return Result{}, err
	}

	if result.Commit != commit {
		return Result{}, invalid("commit", "not the audited commit")
	}

	return result, nil
}

// invalid is a refusal of the field at path for the rule it broke.
func invalid(path, rule string) error {
	return fmt.Errorf("%s: %s: %w", path, rule, ErrResultInvalid)
}

// fields returns an object's members by name, and refuses a value that is no
// object, an unknown or duplicate key and a missing field.
func fields(v jsonValue, path string, names ...string) (map[string]jsonValue, error) {
	if v.kind != jsonObject {
		return nil, invalid(path, "not an object")
	}

	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[name] = true
	}

	out := make(map[string]jsonValue, len(names))

	for _, m := range v.members {
		if !want[m.key] {
			return nil, invalid(path, "an unknown field")
		}

		if _, seen := out[m.key]; seen {
			return nil, invalid(path, "a duplicate key")
		}

		out[m.key] = m.value
	}

	for _, name := range names {
		if _, ok := out[name]; !ok {
			return nil, invalid(path, "the field "+name+" is missing")
		}
	}

	return out, nil
}

func jsonStr(v jsonValue, path string) (string, error) {
	if v.kind != jsonString {
		return "", invalid(path, "not a string")
	}

	if v.invalid {
		return "", invalid(path, "not valid UTF-8")
	}

	return v.text, nil
}

func jsonBoolean(v jsonValue, path string) (bool, error) {
	if v.kind != jsonBool {
		return false, invalid(path, "not a boolean")
	}

	return v.boolean, nil
}

func jsonInt(v jsonValue, path string) (int, error) {
	if v.kind != jsonNumber || !integerLiteral.MatchString(v.text) {
		return 0, invalid(path, "not an integer")
	}

	n, err := strconv.Atoi(v.text)
	if err != nil {
		return 0, invalid(path, "not an integer")
	}

	return n, nil
}

func jsonList(v jsonValue, path string, limit int) ([]jsonValue, error) {
	if v.kind != jsonArray {
		return nil, invalid(path, "not an array")
	}

	if len(v.items) > limit {
		return nil, invalid(path, "more than "+strconv.Itoa(limit)+" items")
	}

	return v.items, nil
}

func jsonStrings(v jsonValue, path string, limit int) ([]string, error) {
	items, err := jsonList(v, path, limit)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(items))

	for i, item := range items {
		s, err := jsonStr(item, path+"["+strconv.Itoa(i)+"]")
		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return out, nil
}

// decodeResult turns the tree into a Result, refusing every structural rule:
// unknown, duplicate and missing fields, null, a value of another type,
// invalid UTF-8 and a list over its cap. validateResult checks the values.
func decodeResult(tree jsonValue) (Result, error) {
	top, err := fields(tree, "result",
		"schema", "commit", "release_tag", "image_digest", "tools", "checks", "findings", "changes", "limits")
	if err != nil {
		return Result{}, err
	}

	var r Result

	if r.Schema, err = jsonInt(top["schema"], "schema"); err != nil {
		return Result{}, err
	}

	if r.Commit, err = jsonStr(top["commit"], "commit"); err != nil {
		return Result{}, err
	}

	if r.ReleaseTag, err = jsonStr(top["release_tag"], "release_tag"); err != nil {
		return Result{}, err
	}

	if r.ImageDigest, err = jsonStr(top["image_digest"], "image_digest"); err != nil {
		return Result{}, err
	}

	if r.Tools, err = decodeTools(top["tools"]); err != nil {
		return Result{}, err
	}

	if r.Checks, err = decodeChecks(top["checks"]); err != nil {
		return Result{}, err
	}

	if r.Findings, err = decodeFindings(top["findings"]); err != nil {
		return Result{}, err
	}

	if r.Changes, err = decodeChanges(top["changes"]); err != nil {
		return Result{}, err
	}

	if r.Limits, err = jsonStrings(top["limits"], "limits", maxLimits); err != nil {
		return Result{}, err
	}

	return r, nil
}

func decodeTools(v jsonValue) ([]Tool, error) {
	items, err := jsonList(v, "tools", maxTools)
	if err != nil {
		return nil, err
	}

	var tools []Tool

	for i, item := range items {
		path := "tools[" + strconv.Itoa(i) + "]"

		f, err := fields(item, path, "name", "version")
		if err != nil {
			return nil, err
		}

		var t Tool

		if t.Name, err = jsonStr(f["name"], path+".name"); err != nil {
			return nil, err
		}

		if t.Version, err = jsonStr(f["version"], path+".version"); err != nil {
			return nil, err
		}

		tools = append(tools, t)
	}

	return tools, nil
}

func decodeChecks(v jsonValue) ([]Check, error) {
	// Each check id once, so a list longer than the vocabulary holds a
	// repeated or an unknown id; validateResult names which.
	items, err := jsonList(v, "checks", len(checkNames)+1)
	if err != nil {
		return nil, err
	}

	var checks []Check

	for i, item := range items {
		path := "checks[" + strconv.Itoa(i) + "]"

		f, err := fields(item, path, "id", "ran", "not_run")
		if err != nil {
			return nil, err
		}

		var c Check

		if c.ID, err = jsonStr(f["id"], path+".id"); err != nil {
			return nil, err
		}

		if c.Ran, err = jsonBoolean(f["ran"], path+".ran"); err != nil {
			return nil, err
		}

		if c.NotRun, err = jsonStr(f["not_run"], path+".not_run"); err != nil {
			return nil, err
		}

		checks = append(checks, c)
	}

	return checks, nil
}

func decodeFindings(v jsonValue) ([]Finding, error) {
	items, err := jsonList(v, "findings", FindingLimit)
	if err != nil {
		return nil, err
	}

	var findings []Finding

	for i, item := range items {
		f, err := decodeFinding(item, "findings["+strconv.Itoa(i)+"]")
		if err != nil {
			return nil, err
		}

		findings = append(findings, f)
	}

	return findings, nil
}

func decodeFinding(v jsonValue, path string) (Finding, error) {
	m, err := fields(v, path,
		"check", "rule", "file", "line", "sink", "severity", "confirmed", "category", "private",
		"scenario", "evidence", "fix", "dismissal")
	if err != nil {
		return Finding{}, err
	}

	var f Finding

	strs := []struct {
		name string
		dst  *string
	}{
		{"check", &f.Check},
		{"rule", &f.Rule},
		{"file", &f.File},
		{"sink", &f.Sink},
		{"severity", &f.Severity},
		{"category", &f.Category},
		{"scenario", &f.Scenario},
		{"evidence", &f.Evidence},
		{"fix", &f.Fix},
		{"dismissal", &f.Dismissal},
	}

	for _, s := range strs {
		if *s.dst, err = jsonStr(m[s.name], path+"."+s.name); err != nil {
			return Finding{}, err
		}
	}

	if f.Line, err = jsonInt(m["line"], path+".line"); err != nil {
		return Finding{}, err
	}

	if f.Confirmed, err = jsonBoolean(m["confirmed"], path+".confirmed"); err != nil {
		return Finding{}, err
	}

	if f.Private, err = jsonBoolean(m["private"], path+".private"); err != nil {
		return Finding{}, err
	}

	return f, nil
}

func decodeChanges(v jsonValue) (Changes, error) {
	m, err := fields(v, "changes", "dependencies", "narrative")
	if err != nil {
		return Changes{}, err
	}

	var c Changes

	if c.Dependencies, err = jsonStrings(m["dependencies"], "changes.dependencies", maxDependencies); err != nil {
		return Changes{}, err
	}

	if c.Narrative, err = jsonStr(m["narrative"], "changes.narrative"); err != nil {
		return Changes{}, err
	}

	return c, nil
}

// validateResult checks every value rule of the schema over a Result, decoded
// or built by a caller: RenderReport runs it too, so no text reaches a report
// that ParseResult would have refused. It checks the commit's shape, not that
// it is the audited one.
func validateResult(r Result) error {
	if r.Schema != SchemaVersion {
		return invalid("schema", "not schema version 1")
	}

	if !commitPattern.MatchString(r.Commit) {
		return invalid("commit", "not 40 lowercase hex characters")
	}

	if !tagPattern.MatchString(r.ReleaseTag) {
		return invalid("release_tag", "not a release tag vMAJOR.MINOR.PATCH")
	}

	if r.ImageDigest != "" && !digestPattern.MatchString(r.ImageDigest) {
		return invalid("image_digest", "not a sha256 digest")
	}

	if err := validateTools(r.Tools); err != nil {
		return err
	}

	ran, err := validateChecks(r.Checks)
	if err != nil {
		return err
	}

	if len(r.Findings) > FindingLimit {
		return invalid("findings", "more than "+strconv.Itoa(FindingLimit)+" findings")
	}

	for i, f := range r.Findings {
		if err := validateFinding(f, "findings["+strconv.Itoa(i)+"]", ran); err != nil {
			return err
		}
	}

	return validateChanges(r.Changes, r.Limits)
}

func validateTools(tools []Tool) error {
	if len(tools) > maxTools {
		return invalid("tools", "more than "+strconv.Itoa(maxTools)+" tools")
	}

	seen := make(map[string]bool, len(tools))

	for i, t := range tools {
		path := "tools[" + strconv.Itoa(i) + "]"

		if !toolNames[t.Name] {
			return invalid(path+".name", "not a tool the schema names")
		}

		if seen[t.Name] {
			return invalid("tools", "a tool named twice")
		}

		seen[t.Name] = true

		if !versionPattern.MatchString(t.Version) {
			return invalid(path+".version", "not a version of up to four numeric parts")
		}
	}

	return nil
}

// validateChecks checks that every check id is listed exactly once with a
// consistent reason, and returns the ids that ran.
func validateChecks(checks []Check) (map[string]bool, error) {
	seen := make(map[string]bool, len(checkNames))
	ran := make(map[string]bool, len(checkNames))

	for i, c := range checks {
		path := "checks[" + strconv.Itoa(i) + "]"

		if !isCheckID(c.ID) {
			return nil, invalid(path+".id", "not a check id")
		}

		if seen[c.ID] {
			return nil, invalid(path+".id", "a check id listed twice")
		}

		seen[c.ID] = true

		switch {
		case c.Ran && c.NotRun != "":
			return nil, invalid(path+".not_run", "not empty for a check that ran")
		case !c.Ran && c.NotRun == "":
			return nil, invalid(path+".not_run", "empty for a check that did not run")
		case !c.Ran && whyNotRun[c.NotRun] == "":
			return nil, invalid(path+".not_run", "not a why id")
		}

		ran[c.ID] = c.Ran
	}

	if len(seen) != len(checkNames) {
		return nil, invalid("checks", "a check id is missing")
	}

	return ran, nil
}

func validateFinding(f Finding, path string, ran map[string]bool) error {
	switch {
	case !isCheckID(f.Check):
		return invalid(path+".check", "not a check id")
	case !ran[f.Check]:
		return invalid(path+".check", "a check that did not run")
	}

	single := []struct {
		name, value string
		lo, hi      int
	}{
		{"rule", f.Rule, 1, 80},
		{"file", f.File, 1, maxFileBytes},
		{"sink", f.Sink, 1, 120},
		{"severity", f.Severity, 0, maxSeverityBytes},
		{"category", f.Category, 0, maxCategoryBytes},
		{"dismissal", f.Dismissal, 0, maxDismissal},
	}

	for _, s := range single {
		if err := checkText(path+"."+s.name, s.value, false, s.lo, s.hi); err != nil {
			return err
		}
	}

	multi := []struct {
		name, value string
		lo, hi      int
	}{
		{"scenario", f.Scenario, 1, maxScenarioBytes},
		{"evidence", f.Evidence, 0, maxEvidenceBytes},
		{"fix", f.Fix, 1, maxFixBytes},
	}

	for _, s := range multi {
		if err := checkText(path+"."+s.name, s.value, true, s.lo, s.hi); err != nil {
			return err
		}
	}

	switch {
	case !rulePattern.MatchString(f.Rule):
		return invalid(path+".rule", "not a rule name")
	case !validFile(f.File):
		return invalid(path+".file", "not a repository path, artifact name or commit")
	case f.Line < 0 || f.Line > maxLine:
		return invalid(path+".line", "not from 0 to 9999999")
	case !sinkPattern.MatchString(f.Sink):
		return invalid(path+".sink", "not a sink name")
	}

	return nil
}

// validFile is the schema's file rule: 1 to 200 bytes of the allowed set,
// starting with a letter, a digit or _, with no empty, . or .. segment.
func validFile(file string) bool {
	if len(file) == 0 || len(file) > maxFileBytes || !filePattern.MatchString(file) {
		return false
	}

	for _, segment := range strings.Split(file, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}

	return true
}

func validateChanges(c Changes, limits []string) error {
	if len(c.Dependencies) > maxDependencies {
		return invalid("changes.dependencies", "more than "+strconv.Itoa(maxDependencies)+" dependencies")
	}

	for i, d := range c.Dependencies {
		if err := checkText("changes.dependencies["+strconv.Itoa(i)+"]", d, false, 1, maxDependency); err != nil {
			return err
		}
	}

	if err := checkText("changes.narrative", c.Narrative, true, 0, maxNarrative); err != nil {
		return err
	}

	if len(limits) > maxLimits {
		return invalid("limits", "more than "+strconv.Itoa(maxLimits)+" limits")
	}

	for i, l := range limits {
		if err := checkText("limits["+strconv.Itoa(i)+"]", l, false, 1, maxLimitBytes); err != nil {
			return err
		}
	}

	return nil
}

// checkText checks a string's encoding, its characters and its length in
// bytes. A field holds printable ASCII alone, and a multi-line field also LF
// and TAB (textRune).
func checkText(path, s string, multiLine bool, lo, hi int) error {
	if !utf8.ValidString(s) {
		return invalid(path, "not valid UTF-8")
	}

	for _, r := range s {
		if (r == '\n' || r == '\t') && multiLine {
			continue
		}

		if !textRune(r) {
			return invalid(path, "a character outside printable ASCII")
		}
	}

	switch {
	case len(s) < lo && lo == 1:
		return invalid(path, "empty")
	case len(s) < lo:
		return invalid(path, "shorter than "+strconv.Itoa(lo)+" bytes")
	case len(s) > hi:
		return invalid(path, "longer than "+strconv.Itoa(hi)+" bytes")
	}

	return nil
}

// textRune reports whether r is printable ASCII, U+0020 to U+007E: the
// only characters a field holds besides a multi-line field's LF and TAB (ADR
// 0040, "The text rule"). A lookalike letter, a mark with no glyph of its own,
// a typographic dash or quote and a rune that renders blank all carry a token
// past the maintainer's eye, and no list of characters to refuse is complete,
// so the rule is an allow list. It holds no control, no bidirectional or
// invisible character and no space but U+0020.
func textRune(r rune) bool {
	return r >= 0x20 && r <= 0x7e
}
