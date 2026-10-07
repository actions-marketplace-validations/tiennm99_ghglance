// Package web serves the ghglance web UI: a form that queues card
// generation for any GitHub user, and pages that re-show the stored cards.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tiennm99/ghglance/internal/card"
	"github.com/tiennm99/ghglance/internal/theme"
)

//go:embed templates static
var assets embed.FS

// maxFormBytes bounds a form post; the real form is well under 1 KiB.
const maxFormBytes = 16 << 10

// Submission rate per client IP: a burst of 5, then one every 2 minutes.
const (
	submitBurst    = 5
	submitInterval = 2 * time.Minute
)

const (
	pageCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; " +
		"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	// Cards are static drawings: no scripts, no external fetches. Inline
	// style attributes are the only thing they need.
	svgCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"
)

// Config configures the web server.
type Config struct {
	Addr     string
	DataDir  string
	Cooldown time.Duration
	// Retention is how long generated cards are kept before they are
	// deleted (0 = forever).
	Retention time.Duration
	Workers   int
	// JobTimeout bounds one generation job (0 = no limit).
	JobTimeout time.Duration
	// Token is the server's own GitHub token, used for submissions that
	// bring none. It never renders private or org-administered data.
	Token string
	// Fetcher overrides the GitHub fetch; nil uses the real API.
	Fetcher Fetcher
}

// Server holds the store, job queue and handlers.
type Server struct {
	cfg     Config
	store   *Store
	queue   *Queue
	limiter *rateLimiter
	pages   map[string]*template.Template
	static  http.Handler
}

// New opens the data directory and starts the job workers.
func New(cfg Config) (*Server, error) {
	if cfg.Fetcher == nil {
		cfg.Fetcher = githubFetcher{}
	}
	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	pages, err := parsePages()
	if err != nil {
		store.Close()
		return nil, err
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		store.Close()
		return nil, err
	}
	return &Server{
		cfg:     cfg,
		store:   store,
		queue:   newQueue(store, cfg.Fetcher, cfg.Token, cfg.JobTimeout, cfg.Cooldown, cfg.Workers),
		limiter: newRateLimiter(submitBurst, submitInterval),
		pages:   pages,
		static:  http.FileServerFS(staticFS),
	}, nil
}

// Close stops the workers and releases the data directory.
func (s *Server) Close() {
	s.queue.Stop()
	s.store.Close()
}

// Run serves until ctx is cancelled, then drains in-flight requests and
// stops the job workers. Queued and running jobs are abandoned; nothing
// half-rendered is ever published.
func Run(ctx context.Context, cfg Config) error {
	s, err := New(cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	if cfg.Token == "" {
		log.Printf("warn: GITHUB_TOKEN is empty; only submissions with their own token will succeed")
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Printf("serving on %s, data in %s", ln.Addr(), s.store.dir)
	// Check the server token up front so a private-capable token is
	// reported at startup, not on the first submission.
	go s.queue.server(ctx)
	go s.expireLoop(ctx)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// Handler returns the routed, hardened HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("POST /generate", s.handleGenerate)
	mux.HandleFunc("GET /u/{user}", s.handleUser)
	mux.HandleFunc("GET /u/{user}/status", s.handleStatus)
	mux.HandleFunc("GET /u/{user}/{theme}/{card}", s.handleCard)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.withCache(s.static, "public, max-age=3600")))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.renderMessage(w, http.StatusNotFound, "Page not found", "There is nothing at this address.")
	})

	cop := http.NewCrossOriginProtection()
	return commonHeaders(cop.Handler(mux))
}

func commonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", pageCSP)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withCache(next http.Handler, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

// pageData feeds every template.
type pageData struct {
	Title   string
	Heading string
	Message string
	Error   string
	Form    formValues

	Login        string
	Key          string
	Meta         *Meta
	Job          JobStatus
	Notice       string
	CooldownLeft string
	ExpiresIn    string
	Themes       []string
	Theme        string
	Cards        []cardView
	Markdown     string
}

type cardView struct {
	Name string
	Src  string
	URL  string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index", pageData{Title: "ghglance", Form: defaultForm()})
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.render(w, http.StatusBadRequest, "index", pageData{Title: "ghglance", Error: "The form could not be read.", Form: defaultForm()})
		return
	}
	sub, form, err := parseSubmission(r.PostForm.Get)
	if !s.limiter.Allow(clientKey(r)) {
		w.Header().Set("Retry-After", strconv.Itoa(int(submitInterval.Seconds())))
		s.render(w, http.StatusTooManyRequests, "index", pageData{
			Title: "ghglance",
			Error: "Too many submissions from your address. Wait a couple of minutes and try again.",
			Form:  form,
		})
		return
	}
	if err != nil {
		s.render(w, http.StatusBadRequest, "index", pageData{Title: "ghglance", Error: capitalize(err.Error()) + ".", Form: form})
		return
	}

	if sub.Token == "" && s.cfg.Token == "" {
		s.render(w, http.StatusBadRequest, "index", pageData{
			Title: "ghglance",
			Error: "This server has no GitHub token of its own. Add yours under Options.",
			Form:  form,
		})
		return
	}

	target := "/u/" + url.PathEscape(userKey(sub.Login))
	if s.queue.Status(sub.Login).Active() {
		http.Redirect(w, r, target+"?notice=pending", http.StatusSeeOther)
		return
	}
	if sub.Token == "" && s.store.cooldownLeft(sub.Login, s.cfg.Cooldown, time.Now()) > 0 {
		http.Redirect(w, r, target+"?notice=fresh", http.StatusSeeOther)
		return
	}
	created, err := s.queue.Submit(sub)
	if err != nil {
		s.render(w, http.StatusServiceUnavailable, "index", pageData{Title: "ghglance", Error: capitalize(err.Error()) + ".", Form: form})
		return
	}
	if !created {
		target += "?notice=pending"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	login := r.PathValue("user")
	if !validUsername(login) {
		s.renderMessage(w, http.StatusNotFound, "Not a GitHub username", "GitHub usernames use letters, digits and single hyphens, up to 39 characters.")
		return
	}
	meta, err := s.store.Meta(login)
	if err != nil && !isNotExist(err) {
		log.Printf("read meta %s: %v", userKey(login), err)
		s.renderMessage(w, http.StatusInternalServerError, "Something went wrong", "The stored cards could not be read.")
		return
	}
	job := s.queue.Status(login)
	if meta == nil && job.State == stateNone {
		form := defaultForm()
		form.User = login
		s.render(w, http.StatusNotFound, "index", pageData{
			Title:   "ghglance",
			Message: fmt.Sprintf("No cards for %s yet. Generate them below.", login),
			Form:    form,
		})
		return
	}

	d := pageData{
		Title:  login + " · ghglance",
		Login:  login,
		Key:    userKey(login),
		Meta:   meta,
		Job:    job,
		Themes: theme.IDs(),
		Theme:  defaultTheme,
		Form:   defaultForm(),
	}
	d.Form.User = login
	if t := r.URL.Query().Get("theme"); validTheme(t) {
		d.Theme = t
	}
	switch r.URL.Query().Get("notice") {
	case "fresh":
		d.Notice = "These cards are recent, so they were not regenerated."
	case "pending":
		if job.Active() {
			d.Notice = "A generation for this user is already in progress."
		}
	}
	if meta != nil {
		d.Login = meta.Login
		d.Form = formFromOptions(meta.Login, meta.Options)
		if left := s.store.cooldownLeft(login, s.cfg.Cooldown, time.Now()); left > 0 {
			d.CooldownLeft = humanDuration(left)
		}
		if s.cfg.Retention > 0 {
			d.ExpiresIn = humanDuration(max(meta.GeneratedAt.Add(s.cfg.Retention).Sub(time.Now()), time.Minute))
		}
		base := baseURL(r)
		version := strconv.FormatInt(meta.GeneratedAt.Unix(), 10)
		var md strings.Builder
		for _, f := range card.Filenames() {
			rel := "/u/" + d.Key + "/" + d.Theme + "/" + f
			name := strings.ReplaceAll(strings.TrimSuffix(f, ".svg"), "-", " ")
			d.Cards = append(d.Cards, cardView{Name: name, Src: rel + "?v=" + version, URL: base + rel})
			fmt.Fprintf(&md, "![%s](%s)\n", name, base+rel)
		}
		d.Markdown = md.String()
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, http.StatusOK, "user", d)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	login := r.PathValue("user")
	if !validUsername(login) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(s.queue.Status(login))
}

func (s *Server) handleCard(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.OpenCard(r.PathValue("user"), r.PathValue("theme"), r.PathValue("card"))
	if err != nil {
		if !isNotExist(err) {
			log.Printf("open card %s: %v", r.URL.Path, err)
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/svg+xml")
	h.Set("Content-Security-Policy", svgCSP)
	h.Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (s *Server) render(w http.ResponseWriter, status int, page string, d pageData) {
	var buf strings.Builder
	if err := s.pages[page].ExecuteTemplate(&buf, "layout", d); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, buf.String())
}

func (s *Server) renderMessage(w http.ResponseWriter, status int, heading, message string) {
	s.render(w, status, "message", pageData{Title: heading + " · ghglance", Heading: heading, Message: message})
}

func parsePages() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"fmtTime": func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"index", "user", "message"} {
		t, err := template.New(name).Funcs(funcs).ParseFS(assets,
			"templates/layout.html", "templates/form.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse %s template: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

// formFromOptions pre-fills the regenerate form with a set's last options.
// Private scope stays ticked: it only takes effect with a token anyway.
func formFromOptions(login string, o Options) formValues {
	return formValues{
		User:            login,
		TZ:              o.TZ,
		StartOfWeek:     o.StartOfWeek,
		IncludeForks:    o.IncludeForks,
		IncludeOrgRepos: o.IncludeOrgRepos,
		IncludePrivate:  true,
		CommitsPerRepo:  strconv.Itoa(o.CommitsPerRepo),
	}
}

// baseURL is the absolute origin for copyable embed links. The scheme
// follows the proxy's X-Forwarded-Proto when one sits in front.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	}
	return "under a minute"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// expireInterval is how often expired cards are swept while serving.
const expireInterval = time.Hour

// expireLoop deletes cards older than the retention once at startup and then
// every expireInterval until ctx is cancelled.
func (s *Server) expireLoop(ctx context.Context) {
	if s.cfg.Retention <= 0 {
		return
	}
	t := time.NewTicker(expireInterval)
	defer t.Stop()
	for {
		if n := s.store.Expire(s.cfg.Retention, time.Now()); n > 0 {
			log.Printf("expired cards for %d user(s) older than %s", n, s.cfg.Retention)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
