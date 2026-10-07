package web

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tiennm99/ghglance/internal/card"
	"github.com/tiennm99/ghglance/internal/github"
	"github.com/tiennm99/ghglance/internal/theme"
)

// GitHub logins: alphanumerics separated by single hyphens, 1–39 chars, no
// leading or trailing hyphen. Anything passing this is also a safe single
// path segment, which is what lets it name a directory under the data dir.
var usernameRE = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

// Sign-in tokens are only ever forwarded in an Authorization header;
// restricting the charset of what GitHub's token endpoint returns rules out
// header injection.
var tokenRE = regexp.MustCompile(`^[A-Za-z0-9_]{20,255}$`)

// IANA zone names: letters, digits and _ + - / only. time.LoadLocation
// refuses ".." itself; the charset keeps odd input out of the zoneinfo
// lookup entirely.
var tzRE = regexp.MustCompile(`^[A-Za-z0-9_+\-/]{1,64}$`)

const (
	defaultTheme          = "dracula"
	defaultCommitsPerRepo = 500
	maxCommitsPerRepo     = 5000
)

// cardFiles is the set of servable card basenames, built from the renderer.
var cardFiles = func() map[string]bool {
	m := map[string]bool{}
	for _, f := range card.Filenames() {
		m[f] = true
	}
	return m
}()

// validUsername reports whether s is a syntactically valid GitHub login.
func validUsername(s string) bool {
	return len(s) >= 1 && len(s) <= 39 && usernameRE.MatchString(s)
}

// validTheme reports whether id names a registered theme.
func validTheme(id string) bool {
	_, ok := theme.Lookup(id)
	return ok
}

// validCard reports whether name is a rendered card basename ("stats.svg").
func validCard(name string) bool {
	return cardFiles[name]
}

// Options are the generation settings recorded in meta.json. They never
// include the sign-in token.
type Options struct {
	TZ              string `json:"tz"`
	StartOfWeek     string `json:"start_of_week"`
	IncludeForks    bool   `json:"include_forks"`
	IncludeOrgRepos bool   `json:"include_org_repos"`
	IncludePrivate  bool   `json:"include_private"`
	CommitsPerRepo  int    `json:"commits_per_repo"`
}

// submission is a validated form post. Token is the sign-in token GitHub
// issues at the callback; it is revoked when the job ends.
type submission struct {
	Login   string
	Token   string
	Options Options
}

// formValues is the raw form, kept so a rejected post re-renders as typed.
type formValues struct {
	User            string
	TZ              string
	StartOfWeek     string
	IncludeForks    bool
	IncludeOrgRepos bool
	IncludePrivate  bool
	CommitsPerRepo  string
}

// defaultForm is the blank generation form. Private repos start unticked: a
// sign-in asks for exactly what is ticked, and repo is a broad grant that
// only helps when the username is the visitor's own account.
func defaultForm() formValues {
	return formValues{
		TZ:             "UTC",
		StartOfWeek:    "sunday",
		IncludeForks:   true,
		CommitsPerRepo: strconv.Itoa(defaultCommitsPerRepo),
	}
}

// parseSubmission validates a generation form. The ticked scope is kept:
// it picks the scopes the sign-in asks for, and the job still enforces who
// the resulting token belongs to.
func parseSubmission(get func(string) string) (submission, formValues, error) {
	f := formValues{
		User:            strings.TrimSpace(get("user")),
		TZ:              strings.TrimSpace(get("tz")),
		StartOfWeek:     strings.TrimSpace(get("start_of_week")),
		IncludeForks:    get("include_forks") != "",
		IncludeOrgRepos: get("include_org_repos") != "",
		IncludePrivate:  get("include_private") != "",
		CommitsPerRepo:  strings.TrimSpace(get("commits_per_repo")),
	}

	if !validUsername(f.User) {
		return submission{}, f, errors.New("enter a valid GitHub username: letters, digits and single hyphens, up to 39 characters")
	}

	tz := f.TZ
	if tz == "" {
		tz = "UTC"
	}
	if tz == "Local" || !tzRE.MatchString(tz) {
		return submission{}, f, fmt.Errorf("unknown timezone %q", tz)
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return submission{}, f, fmt.Errorf("unknown timezone %q", tz)
	}

	wd, err := github.ParseWeekday(f.StartOfWeek)
	if err != nil {
		return submission{}, f, errors.New("unknown start of week")
	}

	perRepo := defaultCommitsPerRepo
	if f.CommitsPerRepo != "" {
		n, err := strconv.Atoi(f.CommitsPerRepo)
		if err != nil || n < 0 || n > maxCommitsPerRepo {
			return submission{}, f, fmt.Errorf("commits per repo must be between 0 and %d", maxCommitsPerRepo)
		}
		perRepo = n
	}

	return submission{Login: f.User, Options: Options{
		TZ:              tz,
		StartOfWeek:     strings.ToLower(wd.String()),
		IncludeForks:    f.IncludeForks,
		IncludeOrgRepos: f.IncludeOrgRepos,
		IncludePrivate:  f.IncludePrivate,
		CommitsPerRepo:  perRepo,
	}}, f, nil
}

// collectConfig maps validated options onto the shared fetch pipeline.
func (o Options) collectConfig() github.CollectConfig {
	loc, err := time.LoadLocation(o.TZ)
	if err != nil {
		loc = time.UTC
	}
	wd, _ := github.ParseWeekday(o.StartOfWeek)
	return github.CollectConfig{
		Options: github.FetchOptions{
			IncludeForks:    o.IncludeForks,
			IncludePrivate:  o.IncludePrivate,
			IncludeOrgRepos: o.IncludeOrgRepos,
		},
		Location:       loc,
		WeekStart:      wd,
		CommitsPerRepo: o.CommitsPerRepo,
	}
}
