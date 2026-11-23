package publish

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Run is the workflow run a report belongs to, as NewRun accepted it.
type Run struct {
	// ServerURL is GITHUB_SERVER_URL: https:// and a host name.
	ServerURL string
	// Repository is GITHUB_REPOSITORY: owner/name.
	Repository string
	// ID is GITHUB_RUN_ID: a decimal of up to 20 digits.
	ID string
	// Date is the run's UTC date, YYYY-MM-DD.
	Date string
}

var (
	serverPattern     = regexp.MustCompile(`^https://[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	runIDPattern      = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	datePattern       = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})$`)
)

// NewRun returns the run, and refuses (ErrConfig) a server URL other than
// https:// and a host name, a repository other than owner/name of
// [A-Za-z0-9._-], a run ID other than a decimal of up to 20 digits with no
// leading zero, and a date other than a real YYYY-MM-DD.
func NewRun(serverURL, repository, runID, date string) (Run, error) {
	if !serverPattern.MatchString(serverURL) {
		return Run{}, fmt.Errorf("GITHUB_SERVER_URL: not https:// and a host name: %w", ErrConfig)
	}

	if !repositoryPattern.MatchString(repository) {
		return Run{}, fmt.Errorf("GITHUB_REPOSITORY: not owner/name: %w", ErrConfig)
	}

	for _, part := range strings.Split(repository, "/") {
		if part == "." || part == ".." {
			return Run{}, fmt.Errorf("GITHUB_REPOSITORY: a dot segment: %w", ErrConfig)
		}
	}

	if !runIDPattern.MatchString(runID) {
		return Run{}, fmt.Errorf("GITHUB_RUN_ID: not a decimal of up to 20 digits: %w", ErrConfig)
	}

	if _, ok := parseDate(date); !ok {
		return Run{}, fmt.Errorf("the run's date: not a real YYYY-MM-DD: %w", ErrConfig)
	}

	return Run{ServerURL: serverURL, Repository: repository, ID: runID, Date: date}, nil
}

// URL returns the run's page: <server>/<repository>/actions/runs/<id>.
func (r Run) URL() string {
	return r.ServerURL + "/" + r.Repository + "/actions/runs/" + r.ID
}

// validate checks a Run its caller may have built by hand.
func (r Run) validate() error {
	_, err := NewRun(r.ServerURL, r.Repository, r.ID, r.Date)

	return err
}

// parseDate reads a real YYYY-MM-DD as midnight UTC; the date must name a
// day the calendar has, so 2026-02-30 is refused.
func parseDate(s string) (time.Time, bool) {
	m := datePattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}

	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])

	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return time.Time{}, false
	}

	return t, true
}
