// ghglance generates SVG cards summarizing a GitHub user's profile.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tiennm99/ghglance/internal/card"
	"github.com/tiennm99/ghglance/internal/github"
	"github.com/tiennm99/ghglance/internal/theme"
	"github.com/tiennm99/ghglance/internal/web"
)

func main() {
	var (
		user           = flag.String("user", "", "GitHub username (required)")
		token          = flag.String("token", os.Getenv("GITHUB_TOKEN"), "GitHub token (or env GITHUB_TOKEN)")
		out            = flag.String("out", "output", "output directory")
		themesFlag     = flag.String("themes", "dracula", "comma-separated theme ids, or 'all'")
		tzName         = flag.String("tz", "Local", "timezone for productive-time card (IANA name, e.g. Asia/Saigon)")
		topRepos       = flag.Int("top-repos", 0, "optional cap on seed repos probed for commit history (0 = unlimited)")
		perRepo        = flag.Int("commits-per-repo", 500, "max commits sampled per repo, 0 = every commit (covers both last-year and all-time aggregates)")
		includeForks   = flag.Bool("include-forks", true, "include forked repos in stats and commit probing")
		includePrivate = flag.Bool("include-private", true, "include private repos (requires PAT with repo scope; silently no-op otherwise)")
		includeOrgs    = flag.Bool("include-org-repos", false, "count org-owned repos you administer toward stars, repo count, repos-per-language and top-starred")
		timeout        = flag.Duration("timeout", 30*time.Minute, "overall deadline for fetch phase; per generation job under -serve (0 = no limit)")
		startOfWeek    = flag.String("start-of-week", "sunday", "first day of week for heatmap rows and weekday bars (sunday|monday|tuesday|…)")
		listThemes     = flag.Bool("list-themes", false, "print available theme ids and exit")
		serve          = flag.String("serve", "", "run the web UI on this address (e.g. :8080) instead of generating once")
		dataDir        = flag.String("data-dir", "data", "web UI: directory holding generated cards")
		cooldown       = flag.Duration("cooldown", 6*time.Hour, "web UI: minimum age of a user's cards before they can be regenerated without the submitter's own token")
		retention      = flag.Duration("retention", 24*time.Hour, "web UI: delete a user's generated cards this long after they were generated (0 = keep forever)")
		workers        = flag.Int("workers", 2, "web UI: concurrent generation jobs")
	)
	flag.Parse()

	if *listThemes {
		for _, id := range theme.IDs() {
			fmt.Println(id)
		}
		return
	}

	if *serve != "" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		err := web.Run(ctx, web.Config{
			Addr:       *serve,
			DataDir:    *dataDir,
			Cooldown:   *cooldown,
			Retention:  *retention,
			Workers:    *workers,
			JobTimeout: *timeout,
			Token:      *token,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *user == "" {
		fmt.Fprintln(os.Stderr, "error: -user is required")
		flag.Usage()
		os.Exit(2)
	}

	selected, err := resolveThemes(*themesFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	loc, err := time.LoadLocation(*tzName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: unknown timezone %q, falling back to UTC\n", *tzName)
		loc = time.UTC
	}

	weekStart, err := github.ParseWeekday(*startOfWeek)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v, falling back to Sunday\n", err)
		weekStart = time.Sunday
	}

	opts := github.FetchOptions{
		IncludeForks:    *includeForks,
		IncludePrivate:  *includePrivate,
		IncludeOrgRepos: *includeOrgs,
	}

	// Overall fetch budget. Ctrl-C cancels in-flight HTTP requests cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *timeout > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, *timeout)
		defer cancelTimeout()
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	profile, err := github.Collect(ctx, github.NewClient(*token), *user, github.CollectConfig{
		Options:        opts,
		Location:       loc,
		WeekStart:      weekStart,
		TopRepos:       *topRepos,
		CommitsPerRepo: *perRepo,
		Warnf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "warn: "+format+"\n", args...)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for _, t := range selected {
		if err := card.RenderAll(profile, t, *out); err != nil {
			fmt.Fprintf(os.Stderr, "error: render %s: %v\n", t.ID, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s/%s/\n", *out, t.ID)
	}
}

func resolveThemes(spec string) ([]theme.Theme, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("no themes specified")
	}
	if spec == "all" {
		ids := theme.IDs()
		out := make([]theme.Theme, 0, len(ids))
		for _, id := range ids {
			if t, ok := theme.Lookup(id); ok {
				out = append(out, t)
			}
		}
		return out, nil
	}
	var out []theme.Theme
	for _, id := range strings.Split(spec, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		t, ok := theme.Lookup(id)
		if !ok {
			return nil, fmt.Errorf("unknown theme %q (use -list-themes)", id)
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid themes")
	}
	return out, nil
}
