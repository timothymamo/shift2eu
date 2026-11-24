package lint_test

import (
	"fmt"
	"path"
	"strings"
)

// allowedSavedPages names the saved-page-shaped files git may track, by their
// slash-separated path relative to the module root: a file with a saved web
// page extension (savedPageExtensions) or a file under a *_files directory.
// A saved price page never belongs here: it is read from a scratch directory
// outside the repository (issue #212). Add an entry only for a file the project
// itself writes and needs in git, such as an HTML fixture a renderer test reads,
// with a comment on the entry saying why it is no saved page.
var allowedSavedPages = map[string]bool{
	// The site's pages are the templates ci/sitebuild renders into the
	// bundle (PRD #297, milestones 2 and the redesign's inventory page, and the
	// About page of issue #528): the project writes them, they load nothing from
	// a third party and they are no saved web page; the builder and the site's
	// static checks read them from git.
	"site/about.html":     true,
	"site/index.html":     true,
	"site/inventory.html": true,
	"site/privacy.html":   true,
	"site/verify.html":    true,
}

// savedPageExtensions are the lower-cased final extensions of the forms a
// browser saves a web page in: HTML and its XHTML and server-side-include
// variants, the single-file MHTML archive and Safari's web archive.
var savedPageExtensions = map[string]bool{
	".html":       true,
	".htm":        true,
	".xhtml":      true,
	".xht":        true,
	".shtml":      true,
	".mhtml":      true,
	".mht":        true,
	".webarchive": true,
}

// savedPageAdvice ends every saved-page problem: where the convention is
// stated and how to clear the problem.
const savedPageAdvice = "a saved price page is read from a scratch directory outside the repository " +
	"and never committed (internal/adapter/target/upcloud/SOURCES.md, issue #212): remove it from " +
	"git, or add it to allowedSavedPages in test/lint/savedpages_rule_test.go with the reason it is " +
	"no saved page"

// savedPageProblems returns one problem per path that allowed does not name and
// that is a saved web page or part of one, in input order: its final extension,
// in any case, is in savedPageExtensions, or a directory segment, in any case,
// ends in _files, the asset directory a "Webpage, Complete" save writes beside
// the page. paths are slash-separated and relative to the module root.
func savedPageProblems(paths []string, allowed map[string]bool) []string {
	var problems []string
	for _, p := range paths {
		if allowed[p] {
			continue
		}
		switch {
		case savedPageExtensions[strings.ToLower(path.Ext(p))]:
			problems = append(problems, fmt.Sprintf("%s: git tracks a saved web page; %s", p, savedPageAdvice))
		case inAssetDirectory(p):
			problems = append(problems, fmt.Sprintf(
				"%s: git tracks a file in the asset directory of a saved web page (a *_files directory); %s",
				p, savedPageAdvice))
		}
	}
	return problems
}

// inAssetDirectory reports whether a directory segment of p, not its file name,
// ends in _files in any case.
func inAssetDirectory(p string) bool {
	for _, segment := range strings.Split(path.Dir(p), "/") {
		if strings.HasSuffix(strings.ToLower(segment), "_files") {
			return true
		}
	}
	return false
}
