package web

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tiennm99/ghglance/internal/github"
)

// testToken is a syntactically valid token that must never reach disk.
const testToken = "ghp_testTOKENvalue0123456789abcdef"

type fetchCall struct {
	token string
	login string
	cfg   github.CollectConfig
}

// fakeFetcher stands in for GitHub. When gate is set, Fetch blocks until
// it is closed, so tests can observe a running job. When stall is set,
// Fetch waits out the job deadline and then returns a partial profile with
// no error, as Collect does when it only warns about a late stage.
// tokens maps a token to the account behind it; an unknown token belongs
// to viewer and cannot read private repos.
type fakeFetcher struct {
	mu     sync.Mutex
	calls  []fetchCall
	viewer string
	tokens map[string]github.TokenInfo
	gate   chan struct{}
	stall  bool
	err    error
}

func (f *fakeFetcher) Fetch(ctx context.Context, token, login string, cfg github.CollectConfig) (*github.Profile, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fetchCall{token: token, login: login, cfg: cfg})
	gate, err, stall := f.gate, f.err, f.stall
	f.mu.Unlock()
	if stall {
		<-ctx.Done()
		return &github.Profile{Login: login}, nil
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &github.Profile{Login: login, Name: "Test User", WeekStart: cfg.WeekStart}, nil
}

func (f *fakeFetcher) TokenInfo(_ context.Context, token string) (github.TokenInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if info, ok := f.tokens[token]; ok {
		return info, nil
	}
	return github.TokenInfo{Login: f.viewer}, nil
}

func (f *fakeFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeFetcher) lastCall() fetchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func newTestServer(t *testing.T, f *fakeFetcher) *Server {
	t.Helper()
	return newTestServerTimeout(t, f, time.Minute)
}

func newTestServerTimeout(t *testing.T, f *fakeFetcher, timeout time.Duration) *Server {
	t.Helper()
	s, err := New(Config{
		DataDir:    t.TempDir(),
		Cooldown:   time.Hour,
		Workers:    2,
		JobTimeout: timeout,
		Token:      "server-token",
		Fetcher:    f,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// waitIdle blocks until login's job has finished.
func waitIdle(t *testing.T, q *Queue, login string) JobStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := q.Status(login); !st.Active() {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job for %s did not finish", login)
	return JobStatus{}
}

func form(kv ...string) func(string) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v.Get
}

func TestValidUsername(t *testing.T) {
	good := []string{"a", "octocat", "tiennm99", "a-b", "A1-b2-C3", strings.Repeat("a", 39)}
	bad := []string{"", "-a", "a-", "a--b", strings.Repeat("a", 40), "..", "../etc", "a/b", `a\b`, "a.b", "a_b", ".gen", "a b", "%2e%2e", "é"}
	for _, s := range good {
		if !validUsername(s) {
			t.Errorf("validUsername(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validUsername(s) {
			t.Errorf("validUsername(%q) = true, want false", s)
		}
	}
}

func TestValidThemeAndCard(t *testing.T) {
	if !validTheme("dracula") || !validTheme("github_dark") {
		t.Error("known themes rejected")
	}
	for _, s := range []string{"", "..", "../dracula", "Dracula", "dracula/"} {
		if validTheme(s) {
			t.Errorf("validTheme(%q) = true", s)
		}
	}
	if !validCard("stats.svg") || !validCard("profile-details.svg") {
		t.Error("known cards rejected")
	}
	for _, s := range []string{"", "stats", "meta.json", "../meta.json", "stats.svg/..", "STATS.svg"} {
		if validCard(s) {
			t.Errorf("validCard(%q) = true", s)
		}
	}
}

func TestParseSubmissionForcesPrivacyWithoutToken(t *testing.T) {
	sub, _, err := parseSubmission(form(
		"user", "octocat", "tz", "Asia/Saigon", "start_of_week", "Mon",
		"include_private", "1", "include_org_repos", "1", "include_forks", "1",
	))
	if err != nil {
		t.Fatal(err)
	}
	if sub.Options.IncludePrivate || sub.Options.IncludeOrgRepos {
		t.Errorf("server-token submission kept private=%v org=%v", sub.Options.IncludePrivate, sub.Options.IncludeOrgRepos)
	}
	if !sub.Options.IncludeForks || sub.Options.StartOfWeek != "monday" || sub.Options.TZ != "Asia/Saigon" {
		t.Errorf("options = %+v", sub.Options)
	}
	if sub.Options.CommitsPerRepo != defaultCommitsPerRepo {
		t.Errorf("commits per repo = %d, want default", sub.Options.CommitsPerRepo)
	}
}

func TestParseSubmissionHonorsOwnToken(t *testing.T) {
	sub, _, err := parseSubmission(form(
		"user", "octocat", "token", testToken, "include_private", "1", "include_org_repos", "1", "commits_per_repo", "0",
	))
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Options.IncludePrivate || !sub.Options.IncludeOrgRepos || sub.Token != testToken {
		t.Errorf("own-token submission = %+v", sub)
	}
	if sub.Options.CommitsPerRepo != 0 {
		t.Errorf("commits per repo = %d, want 0", sub.Options.CommitsPerRepo)
	}
}

func TestParseSubmissionRejects(t *testing.T) {
	cases := map[string]func(string) string{
		"bad user":           form("user", "../etc"),
		"empty user":         form("user", ""),
		"bad tz":             form("user", "a", "tz", "../../etc/passwd"),
		"local tz":           form("user", "a", "tz", "Local"),
		"unknown tz":         form("user", "a", "tz", "Mars/Olympus"),
		"bad week":           form("user", "a", "start_of_week", "moonday"),
		"negative commits":   form("user", "a", "commits_per_repo", "-1"),
		"huge commits":       form("user", "a", "commits_per_repo", "999999"),
		"every commit, no t": form("user", "a", "commits_per_repo", "0"),
		"header injection":   form("user", "a", "token", "ghp_abcdefghijklmnopqrstuvwxyz\r\nX: y"),
	}
	for name, get := range cases {
		if _, _, err := parseSubmission(get); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func publishTest(t *testing.T, s *Store, login string) {
	t.Helper()
	err := s.Publish(&github.Profile{Login: login}, Meta{Login: login, GeneratedAt: time.Now().UTC(), Scope: "public"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStorePublishReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	publishTest(t, s, "Octo-Cat")
	first, _ := os.Readlink(filepath.Join(dir, "octo-cat"))
	publishTest(t, s, "octo-cat")
	second, _ := os.Readlink(filepath.Join(dir, "octo-cat"))
	if first == "" || first == second {
		t.Fatalf("link not replaced: %q -> %q", first, second)
	}
	gens, _ := os.ReadDir(filepath.Join(dir, genDir))
	if len(gens) != 1 {
		t.Errorf("%d generations left, want 1", len(gens))
	}
	f, err := s.OpenCard("OCTO-CAT", "dracula", "stats.svg")
	if err != nil {
		t.Fatalf("open card: %v", err)
	}
	f.Close()
	if m, err := s.Meta("octo-cat"); err != nil || m.Login != "octo-cat" {
		t.Errorf("meta = %+v, %v", m, err)
	}
}

func TestStoreRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.svg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	publishTest(t, s, "octocat")

	probes := [][3]string{
		{"..", "dracula", "stats.svg"},
		{"octocat", "..", "secret.svg"},
		{"octocat", "dracula", "../meta.json"},
		{"octocat", "dracula", "meta.json"},
		{".gen", "dracula", "stats.svg"},
		{"octocat/..", "dracula", "stats.svg"},
	}
	for _, p := range probes {
		if f, err := s.OpenCard(p[0], p[1], p[2]); !errors.Is(err, fs.ErrNotExist) {
			if f != nil {
				f.Close()
			}
			t.Errorf("OpenCard(%q) err = %v, want not-exist", p, err)
		}
	}
	if _, err := s.Meta("../octocat"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Meta(traversal) err = %v", err)
	}
}

func TestStoreSweepsOrphans(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	publishTest(t, s, "octocat")
	s.Close()

	orphan := filepath.Join(dir, genDir, "ghost-1")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".gen/ghost-1", filepath.Join(dir, ".link-ghost-1")); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan generation survived")
	}
	if _, err := os.Lstat(filepath.Join(dir, ".link-ghost-1")); !os.IsNotExist(err) {
		t.Error("temporary link survived")
	}
	if _, err := s.Meta("octocat"); err != nil {
		t.Errorf("live generation swept: %v", err)
	}
}

func TestStoreExpiresOldCards(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	publishTest(t, s, "fresh")
	old := time.Now().UTC().Add(-25 * time.Hour)
	if err := s.Publish(&github.Profile{Login: "stale"}, Meta{Login: "stale", GeneratedAt: old, Scope: "public"}); err != nil {
		t.Fatal(err)
	}

	if n := s.Expire(0, time.Now()); n != 0 {
		t.Errorf("retention 0 removed %d users", n)
	}
	if n := s.Expire(24*time.Hour, time.Now()); n != 1 {
		t.Errorf("removed %d users, want 1", n)
	}
	if _, err := s.Meta("stale"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stale meta err = %v, want not-exist", err)
	}
	if _, err := s.Meta("fresh"); err != nil {
		t.Errorf("fresh cards expired: %v", err)
	}
	gens, _ := os.ReadDir(filepath.Join(dir, genDir))
	if len(gens) != 1 {
		t.Errorf("%d generations left, want 1", len(gens))
	}
}

func TestQueueDedupsPerUser(t *testing.T) {
	f := &fakeFetcher{gate: make(chan struct{})}
	s := newTestServer(t, f)

	sub, _, _ := parseSubmission(form("user", "octocat"))
	if created, err := s.queue.Submit(sub); !created || err != nil {
		t.Fatalf("first submit = %v, %v", created, err)
	}
	again, _, _ := parseSubmission(form("user", "OctoCat"))
	if created, err := s.queue.Submit(again); created || err != nil {
		t.Fatalf("duplicate submit = %v, %v; want deduped", created, err)
	}
	close(f.gate)
	if st := waitIdle(t, s.queue, "octocat"); st.State != stateDone {
		t.Fatalf("state = %+v", st)
	}
	if n := f.callCount(); n != 1 {
		t.Errorf("fetched %d times, want 1", n)
	}
	if created, _ := s.queue.Submit(sub); !created {
		t.Error("finished job blocked a new one")
	}
	waitIdle(t, s.queue, "octocat")
}

func TestQueueTokenHandling(t *testing.T) {
	f := &fakeFetcher{viewer: "Boss", tokens: map[string]github.TokenInfo{
		testToken: {Login: "boss", CanReadPrivate: true},
	}}
	s := newTestServer(t, f)

	// The server token's owner is refused without their own token.
	sub, _, _ := parseSubmission(form("user", "boss"))
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "boss"); st.State != stateFailed || st.Error != errOwner.Error() {
		t.Fatalf("owner job = %+v", st)
	}
	if f.callCount() != 0 {
		t.Fatal("owner was fetched with the server token")
	}

	// Anyone else without a token uses the server token, public scope only.
	sub, _, _ = parseSubmission(form("user", "octocat", "include_private", "1"))
	s.queue.Submit(sub)
	waitIdle(t, s.queue, "octocat")
	c := f.lastCall()
	if c.token != "server-token" || c.cfg.Options.IncludePrivate || c.cfg.Options.IncludeOrgRepos {
		t.Errorf("server-token call = %+v", c)
	}

	// A submitter's token is used for their job and never persisted.
	sub, _, _ = parseSubmission(form("user", "boss", "token", testToken, "include_private", "1"))
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "boss"); st.State != stateDone {
		t.Fatalf("own-token job = %+v", st)
	}
	c = f.lastCall()
	if c.token != testToken || !c.cfg.Options.IncludePrivate || !c.cfg.Strict {
		t.Errorf("own-token call = %+v", c)
	}
	m, err := s.store.Meta("boss")
	if err != nil || m.Scope != "private" {
		t.Errorf("meta = %+v, %v", m, err)
	}
	raw, _ := os.ReadFile(filepath.Join(s.store.dir, "boss", metaFile))
	if strings.Contains(string(raw), testToken) {
		t.Error("token written to meta.json")
	}
	s.queue.mu.Lock()
	leftover := s.queue.jobs["boss"].token
	s.queue.mu.Unlock()
	if leftover != "" {
		t.Error("token kept in memory after the job ended")
	}
}

func TestCooldownAndTokenBypass(t *testing.T) {
	f := &fakeFetcher{tokens: map[string]github.TokenInfo{testToken: {Login: "octocat"}}}
	s := newTestServer(t, f)
	h := s.Handler()
	post := func(v url.Values, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := post(url.Values{"user": {"octocat"}}, "203.0.113.1")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/u/octocat" {
		t.Fatalf("first post = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	waitIdle(t, s.queue, "octocat")

	rec = post(url.Values{"user": {"octocat"}}, "203.0.113.2")
	if loc := rec.Header().Get("Location"); loc != "/u/octocat?notice=fresh" {
		t.Fatalf("cooldown post redirected to %q", loc)
	}
	if n := f.callCount(); n != 1 {
		t.Fatalf("cooldown still fetched: %d calls", n)
	}

	rec = post(url.Values{"user": {"octocat"}, "token": {testToken}}, "203.0.113.3")
	if loc := rec.Header().Get("Location"); loc != "/u/octocat" {
		t.Fatalf("token post redirected to %q", loc)
	}
	waitIdle(t, s.queue, "octocat")
	if n := f.callCount(); n != 2 {
		t.Fatalf("token bypass did not fetch: %d calls", n)
	}

	// Cooldown expiry lets a token-less submission through again.
	if left := s.store.cooldownLeft("octocat", time.Hour, time.Now().Add(2*time.Hour)); left != 0 {
		t.Errorf("cooldown after expiry = %s", left)
	}
}

func TestHandlers(t *testing.T) {
	f := &fakeFetcher{}
	s := newTestServer(t, f)
	h := s.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `action="/generate"`) {
		t.Fatalf("index = %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("index missing security headers")
	}
	if rec := get("/healthz"); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get("/static/app.js"); rec.Code != 200 {
		t.Errorf("static = %d", rec.Code)
	}

	if rec := get("/u/nobody"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "No cards for nobody") {
		t.Errorf("unknown user = %d", rec.Code)
	}
	if rec := get("/u/bad--name"); rec.Code != 404 {
		t.Errorf("invalid user = %d", rec.Code)
	}

	publishTest(t, s.store, "octocat")
	rec = get("/u/octocat?theme=github_dark")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "/u/octocat/github_dark/stats.svg") || !strings.Contains(body, "http://example.com/u/octocat/github_dark/stats.svg") {
		t.Fatalf("user page = %d", rec.Code)
	}
	if rec := get("/u/octocat?theme=../../etc"); !strings.Contains(rec.Body.String(), "/u/octocat/dracula/stats.svg") {
		t.Error("invalid theme not replaced by default")
	}

	rec = get("/u/octocat/dracula/stats.svg")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("card = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "max-age") {
		t.Errorf("card headers = %v", rec.Header())
	}
	if !strings.HasPrefix(rec.Body.String(), "<svg") {
		t.Error("card body is not an SVG")
	}

	for _, p := range []string{
		"/u/octocat/nope/stats.svg",
		"/u/octocat/dracula/nope.svg",
		"/u/octocat/dracula/meta.json",
		"/u/octocat/..%2fmeta.json/stats.svg",
		"/u/..%2f..%2fetc/dracula/stats.svg",
		"/u/octocat/dracula/..%2f..%2fmeta.json",
	} {
		if rec := get(p); rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}

	rec = get("/u/octocat/status")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"none"`) {
		t.Errorf("status = %d %s", rec.Code, rec.Body.String())
	}
}

func TestGenerateValidationAndLimits(t *testing.T) {
	s := newTestServer(t, &fakeFetcher{})
	h := s.Handler()
	post := func(body, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = ip + ":1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("user=..%2Fetc", "198.51.100.1"); rec.Code != 400 {
		t.Errorf("invalid user = %d", rec.Code)
	}
	if rec := post("user=a&token="+testToken, "198.51.100.2"); strings.Contains(rec.Body.String(), testToken) {
		t.Error("token echoed back into the page")
	}
	big := "user=octocat&pad=" + strings.Repeat("x", maxFormBytes)
	if rec := post(big, "198.51.100.3"); rec.Code != 400 {
		t.Errorf("oversized body = %d", rec.Code)
	}

	var last int
	for range submitBurst + 1 {
		last = post("user=--bad", "198.51.100.9").Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("post past burst = %d, want 429", last)
	}
	if rec := post("user=--bad", "198.51.100.10"); rec.Code == http.StatusTooManyRequests {
		t.Error("rate limit leaked across clients")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	now := time.Unix(0, 0)
	l := newRateLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("burst not enforced")
	}
	now = now.Add(time.Minute)
	if !l.Allow("a") || l.Allow("a") {
		t.Error("refill not one per interval")
	}
}

func TestClientKey(t *testing.T) {
	cases := []struct {
		remote, xff, want string
	}{
		{"203.0.113.5:80", "", "203.0.113.5"},
		{"203.0.113.5:80", "1.2.3.4", "203.0.113.5"}, // public peer: header ignored
		{"10.0.1.2:80", "1.2.3.4, 198.51.100.7", "198.51.100.7"},
		{"127.0.0.1:80", "garbage", "127.0.0.1"},
		// IPv6 clients share a bucket per /64, however they rotate.
		{"[2001:db8:1:2:aaaa::1]:80", "", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:bbbb::9]:80", "", "2001:db8:1:2::/64"},
		{"10.0.1.2:80", "2001:db8:5:6:7::1", "2001:db8:5:6::/64"},
		{"10.0.1.2:80", "::ffff:198.51.100.7", "198.51.100.7"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientKey(r); got != c.want {
			t.Errorf("clientKey(%s, %q) = %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}

func TestGenerateNeedsSomeToken(t *testing.T) {
	f := &fakeFetcher{}
	s, err := New(Config{DataDir: t.TempDir(), Workers: 1, Fetcher: f})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	req := httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader("user=octocat"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || f.callCount() != 0 {
		t.Errorf("token-less post on a token-less server = %d, %d fetches", rec.Code, f.callCount())
	}
}

func TestRateLimiterHardCap(t *testing.T) {
	now := time.Unix(0, 0)
	l := newRateLimiter(1, time.Minute)
	l.now = func() time.Time { return now }
	for i := range maxBuckets {
		l.Allow(strconv.Itoa(i))
	}
	if l.Allow("new") {
		t.Fatal("new client admitted past the cap while every bucket is busy")
	}
	if len(l.buckets) != maxBuckets {
		t.Fatalf("table grew to %d", len(l.buckets))
	}
	// Once the old buckets have refilled, a sweep frees room again.
	now = now.Add(time.Hour)
	if !l.Allow("new") {
		t.Error("new client refused after idle buckets could be swept")
	}
	if len(l.buckets) != 1 {
		t.Errorf("sweep left %d buckets", len(l.buckets))
	}
}

func TestServerTokenThatReadsPrivateIsRefused(t *testing.T) {
	f := &fakeFetcher{tokens: map[string]github.TokenInfo{
		"server-token": {Login: "operator", CanReadPrivate: true},
	}}
	s := newTestServer(t, f)
	sub, _, _ := parseSubmission(form("user", "coworker"))
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "coworker"); st.State != stateFailed || st.Error != errServerPrivate.Error() {
		t.Fatalf("job = %+v", st)
	}
	if f.callCount() != 0 {
		t.Error("fetched with a private-capable server token")
	}
	if _, err := s.store.Meta("coworker"); !isNotExist(err) {
		t.Errorf("cards published: %v", err)
	}
}

func TestForeignTokenScope(t *testing.T) {
	const publicToken = "ghp_publicONLYvalue0123456789abcdef"
	f := &fakeFetcher{tokens: map[string]github.TokenInfo{
		testToken:   {Login: "alice", CanReadPrivate: true},
		publicToken: {Login: "alice"},
	}}
	s := newTestServer(t, f)

	// A private-capable token is only good for its own account.
	sub, _, _ := parseSubmission(form("user", "xavier", "token", testToken, "include_private", "1"))
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "xavier"); st.State != stateFailed || !strings.Contains(st.Error, "belongs to alice") {
		t.Fatalf("private-capable foreign token job = %+v", st)
	}
	if f.callCount() != 0 {
		t.Fatal("fetched another user with a private-capable token")
	}

	// A public-only token renders public scope for someone else.
	sub, _, _ = parseSubmission(form("user", "xavier", "token", publicToken, "include_private", "1", "include_org_repos", "1"))
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "xavier"); st.State != stateDone {
		t.Fatalf("public foreign token job = %+v", st)
	}
	if c := f.lastCall(); c.cfg.Options.IncludePrivate || c.cfg.Options.IncludeOrgRepos {
		t.Errorf("foreign token kept private scope: %+v", c.cfg.Options)
	}
	if m, err := s.store.Meta("xavier"); err != nil || m.Scope != "public" || m.Options.IncludePrivate {
		t.Errorf("meta = %+v, %v", m, err)
	}

	// ...and does not skip the cooldown on that account.
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "xavier"); st.State != stateFailed || st.Error != errFresh.Error() {
		t.Fatalf("foreign token during cooldown = %+v", st)
	}
	if n := f.callCount(); n != 1 {
		t.Errorf("fetched %d times, want 1", n)
	}
}

func TestPartialFetchIsNotPublished(t *testing.T) {
	f := &fakeFetcher{tokens: map[string]github.TokenInfo{testToken: {Login: "octocat"}}}
	s := newTestServerTimeout(t, f, 50*time.Millisecond)
	publishTest(t, s.store, "octocat")
	before, err := s.store.Meta("octocat")
	if err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	f.stall = true
	f.mu.Unlock()
	sub, _, _ := parseSubmission(form("user", "octocat", "token", testToken))
	s.queue.Submit(sub)
	if st := waitIdle(t, s.queue, "octocat"); st.State != stateFailed || !strings.Contains(st.Error, "timed out") {
		t.Fatalf("stalled job = %+v", st)
	}
	after, err := s.store.Meta("octocat")
	if err != nil || !after.GeneratedAt.Equal(before.GeneratedAt) {
		t.Errorf("complete set replaced by a partial one: %+v, %v", after, err)
	}
}

func TestPendingNoticeOnlyWhileActive(t *testing.T) {
	f := &fakeFetcher{gate: make(chan struct{}), tokens: map[string]github.TokenInfo{testToken: {Login: "octocat"}}}
	s := newTestServer(t, f)
	h := s.Handler()
	publishTest(t, s.store, "octocat")
	const notice = "already in progress"
	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/u/octocat?notice=pending", nil))
		return rec.Body.String()
	}

	sub, _, _ := parseSubmission(form("user", "octocat", "token", testToken))
	s.queue.Submit(sub)
	if !strings.Contains(get(), notice) {
		t.Error("pending notice missing while the job runs")
	}
	close(f.gate)
	waitIdle(t, s.queue, "octocat")
	if strings.Contains(get(), notice) {
		t.Error("pending notice still shown after the job finished")
	}
}

func TestRateLimitedPostKeepsForm(t *testing.T) {
	s := newTestServer(t, &fakeFetcher{})
	h := s.Handler()
	var rec *httptest.ResponseRecorder
	for range submitBurst + 1 {
		req := httptest.NewRequest(http.MethodPost, "/generate",
			strings.NewReader("user=--bad&tz=Asia%2FSaigon&commits_per_repo=123&token="+testToken))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "198.51.100.20:1"
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
	body := rec.Body.String()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code = %d", rec.Code)
	}
	if !strings.Contains(body, "--bad") || !strings.Contains(body, "Asia/Saigon") || !strings.Contains(body, `value="123"`) {
		t.Error("429 page dropped the submitted form")
	}
	if strings.Contains(body, testToken) {
		t.Error("token echoed back into the 429 page")
	}
}
