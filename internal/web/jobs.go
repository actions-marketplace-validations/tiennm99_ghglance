package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/tiennm99/ghglance/internal/github"
)

// Job states reported by the status endpoint.
const (
	stateQueued  = "queued"
	stateRunning = "running"
	stateDone    = "done"
	stateFailed  = "failed"
	stateNone    = "none"
)

// finishedJobTTL is how long a finished job's status stays queryable, long
// enough for a polling page to see "done" or the failure reason.
const finishedJobTTL = time.Hour

// queueCapacity bounds jobs waiting for a worker; submissions beyond it are
// refused rather than piling up in memory.
const queueCapacity = 64

var (
	errQueueFull     = errors.New("the generation queue is full, try again in a few minutes")
	errStopped       = errors.New("server is shutting down")
	errOwner         = errors.New("this account owns the server's token, so its cards need your own token")
	errServerPrivate = errors.New("this server's GitHub token can read private repositories, so it cannot make public cards; add your own token under Options")
	errFresh         = errors.New("these cards are recent; your token belongs to another account, so it does not skip the wait")
)

// Fetcher runs the GitHub fetch. Tests swap in a fake; the server uses
// github.Collect.
type Fetcher interface {
	Fetch(ctx context.Context, token, login string, cfg github.CollectConfig) (*github.Profile, error)
	TokenInfo(ctx context.Context, token string) (github.TokenInfo, error)
}

type githubFetcher struct{}

func (githubFetcher) Fetch(ctx context.Context, token, login string, cfg github.CollectConfig) (*github.Profile, error) {
	return github.Collect(ctx, github.NewClient(token), login, cfg)
}

func (githubFetcher) TokenInfo(ctx context.Context, token string) (github.TokenInfo, error) {
	return github.NewClient(token).TokenInfo(ctx)
}

// job is one queued generation. token is the submitter's own token, held
// only until the job ends and never logged or persisted.
type job struct {
	key   string
	login string
	opts  Options
	token string

	state    string
	stage    string
	err      string
	queued   time.Time
	started  time.Time
	finished time.Time
}

// JobStatus is the polling view of a job.
type JobStatus struct {
	State    string `json:"state"`
	Stage    string `json:"stage,omitempty"`
	Error    string `json:"error,omitempty"`
	Position int    `json:"position,omitempty"`
	Elapsed  int    `json:"elapsed_seconds,omitempty"`
}

func (s JobStatus) Active() bool { return s.State == stateQueued || s.State == stateRunning }

// Queue runs generation jobs on a fixed worker pool, with at most one queued
// or running job per user.
type Queue struct {
	store       *Store
	fetcher     Fetcher
	serverToken string
	timeout     time.Duration
	cooldown    time.Duration
	now         func() time.Time

	mu      sync.Mutex
	jobs    map[string]*job
	pending []*job
	wake    chan struct{}
	closed  bool

	serverMu   sync.Mutex
	serverInfo *github.TokenInfo

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newQueue(store *Store, fetcher Fetcher, serverToken string, timeout, cooldown time.Duration, workers int) *Queue {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		store:       store,
		fetcher:     fetcher,
		serverToken: serverToken,
		timeout:     timeout,
		cooldown:    cooldown,
		now:         time.Now,
		jobs:        map[string]*job{},
		wake:        make(chan struct{}, queueCapacity),
		ctx:         ctx,
		cancel:      cancel,
	}
	for range workers {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Submit queues a job for sub.Login. When that user already has a queued or
// running job, it is left alone and created is false.
func (q *Queue) Submit(sub submission) (created bool, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false, errStopped
	}
	q.pruneLocked()
	key := userKey(sub.Login)
	if j, ok := q.jobs[key]; ok && (j.state == stateQueued || j.state == stateRunning) {
		return false, nil
	}
	if len(q.pending) >= queueCapacity {
		return false, errQueueFull
	}
	j := &job{
		key:    key,
		login:  sub.Login,
		opts:   sub.Options,
		token:  sub.Token,
		state:  stateQueued,
		queued: q.now(),
	}
	q.jobs[key] = j
	q.pending = append(q.pending, j)
	q.wake <- struct{}{}
	return true, nil
}

// Status reports the job for login, or stateNone when there is none.
func (q *Queue) Status(login string) JobStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[userKey(login)]
	if !ok {
		return JobStatus{State: stateNone}
	}
	st := JobStatus{State: j.state, Stage: j.stage, Error: j.err}
	switch j.state {
	case stateQueued:
		for i, p := range q.pending {
			if p == j {
				st.Position = i + 1
				break
			}
		}
		st.Elapsed = int(q.now().Sub(j.queued).Seconds())
	case stateRunning:
		st.Elapsed = int(q.now().Sub(j.started).Seconds())
	}
	return st
}

// Stop cancels running jobs, drops queued ones and waits for the workers.
func (q *Queue) Stop() {
	q.mu.Lock()
	q.closed = true
	for _, j := range q.pending {
		j.token = ""
		j.state, j.err = stateFailed, errStopped.Error()
	}
	q.pending = nil
	q.mu.Unlock()
	q.cancel()
	q.wg.Wait()
}

// pruneLocked forgets finished jobs older than finishedJobTTL.
func (q *Queue) pruneLocked() {
	cutoff := q.now().Add(-finishedJobTTL)
	for k, j := range q.jobs {
		if (j.state == stateDone || j.state == stateFailed) && j.finished.Before(cutoff) {
			delete(q.jobs, k)
		}
	}
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.wake:
		}
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.mu.Unlock()
			continue
		}
		j := q.pending[0]
		q.pending = q.pending[1:]
		j.state, j.stage, j.started = stateRunning, "fetching from GitHub", q.now()
		q.mu.Unlock()

		err := q.run(j)

		q.mu.Lock()
		j.token = ""
		j.finished = q.now()
		if err != nil {
			j.state, j.stage, j.err = stateFailed, "", err.Error()
		} else {
			j.state, j.stage = stateDone, ""
		}
		q.mu.Unlock()
		if err != nil {
			log.Printf("job %s: failed after %s: %v", j.key, j.finished.Sub(j.started).Round(time.Second), err)
		} else {
			log.Printf("job %s: done in %s", j.key, j.finished.Sub(j.started).Round(time.Second))
		}
	}
}

// run fetches and publishes one job's cards under the per-job timeout.
//
// Published cards are public, so a job only renders what the anonymous
// view of GitHub would show, unless the token belongs to the target user.
// GitHub folds every private contribution a token can see into the totals
// and the calendar, so filtering repo lists alone is not enough: a token
// that can read private repos is refused for anyone but its owner.
func (q *Queue) run(j *job) error {
	ctx := q.ctx
	if q.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, q.timeout)
		defer cancel()
	}

	q.mu.Lock()
	token := j.token
	q.mu.Unlock()
	opts := j.opts
	ownToken := token != ""
	if ownToken {
		info, err := q.fetcher.TokenInfo(ctx, token)
		if err != nil {
			return errors.New("GitHub did not accept your token")
		}
		if !strings.EqualFold(info.Login, j.login) {
			if info.CanReadPrivate {
				return fmt.Errorf("your token belongs to %s and can read private repositories, so it can only generate cards for %s", info.Login, info.Login)
			}
			ownToken = false
			opts.IncludePrivate, opts.IncludeOrgRepos = false, false
			if q.store.cooldownLeft(j.login, q.cooldown, q.now()) > 0 {
				return errFresh
			}
		}
	} else {
		info, err := q.server(ctx)
		if err != nil {
			return errors.New("could not verify the server token, try again later")
		}
		if info.CanReadPrivate {
			return errServerPrivate
		}
		if info.Login != "" && strings.EqualFold(info.Login, j.login) {
			return errOwner
		}
		token = q.serverToken
	}

	cfg := opts.collectConfig()
	cfg.Strict = true
	cfg.Warnf = func(format string, args ...any) {
		log.Printf("job %s: warn: "+format, append([]any{j.key}, args...)...)
	}
	profile, err := q.fetcher.Fetch(ctx, token, j.login, cfg)
	token = ""
	if q.ctx.Err() != nil {
		return errStopped
	}
	if err == nil {
		// A fetch that ran out of time may still hand back a profile; a
		// partial set must never replace a complete one.
		err = ctx.Err()
	}
	if err != nil {
		return publicError(err)
	}

	q.mu.Lock()
	j.stage = "rendering cards"
	q.mu.Unlock()

	scope := "public"
	if ownToken && opts.IncludePrivate {
		scope = "private"
	}
	return q.store.Publish(profile, Meta{
		Login:       j.login,
		GeneratedAt: q.now().UTC(),
		Scope:       scope,
		Options:     opts,
	})
}

// server identifies the server token, cached after the first successful
// lookup. An empty server token has no owner and no reach to protect.
func (q *Queue) server(ctx context.Context) (github.TokenInfo, error) {
	if q.serverToken == "" {
		return github.TokenInfo{}, nil
	}
	q.serverMu.Lock()
	defer q.serverMu.Unlock()
	if q.serverInfo != nil {
		return *q.serverInfo, nil
	}
	info, err := q.fetcher.TokenInfo(ctx, q.serverToken)
	if err != nil {
		log.Printf("server token lookup failed: %v", err)
		return github.TokenInfo{}, err
	}
	if info.CanReadPrivate {
		log.Printf("warn: GITHUB_TOKEN can read private repositories; token-less submissions are refused until it is replaced with a public-only token")
	}
	q.serverInfo = &info
	return info, nil
}

// publicError trims a fetch error to something fit for the status page.
func publicError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timed out while fetching from GitHub")
	}
	msg := err.Error()
	if strings.Contains(msg, "user not found") || strings.Contains(msg, "Could not resolve to a User") {
		return errors.New("GitHub user not found")
	}
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return errors.New(msg)
}
