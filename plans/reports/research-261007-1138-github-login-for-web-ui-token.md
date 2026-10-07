# GitHub login for the web UI token

Researched 2026-10-07 11:38 (Asia/Saigon).

## Answer

Yes. A GitHub **OAuth App** web flow can replace manual token creation.
The user clicks "Sign in with GitHub" and approves once, and the server gets
a token for that one generation. Use an OAuth App rather than a GitHub App:
ghglance's private stats depend on the token seeing every repo the user can
see, and only an OAuth App's `repo` scope gives that.

## Options compared

| Option | Private stats | UX | Depends most on | Fails first when |
| --- | --- | --- | --- | --- |
| **A. OAuth App, token used for one job then revoked** (recommended) | Complete: `repo` covers every repo the user can access | One click and one consent screen | Users accepting the `repo` consent ("full control of private repositories") | A user's org enforces OAuth App access restrictions; org repos drop out until an owner approves the app |
| B. GitHub App user token | Partial: only repos where the app is installed | Install step, then pick repos | Users installing the app on every repo and org | Any org repo without an install, which is common for work repos |
| C. Keep the manual PAT (today) | Complete | Five manual steps on GitHub | Users being willing to create a PAT | Most visitors give up |

Better approaches: none. The requested direction (login to get a token) is
the right one. B looks safer because of fine-grained permissions, but it
cannot see private repos where it isn't installed, so it fails the feature's
purpose.

## Recommended design (A)

```
form ─► POST /auth/start (options saved server-side under a random state, 10 min TTL)
     ─► 302 github.com/login/oauth/authorize?client_id&redirect_uri&scope&state&code_challenge(S256)
     ─► user approves
     ─► GET /auth/callback?code&state ─► check state and PKCE ─► POST /login/oauth/access_token
     ─► token + viewer login ─► existing job queue (same rules as a pasted token)
     ─► job ends ─► DELETE /applications/{client_id}/token (revoke) ─► token gone
```

- **Two buttons.** "Sign in (public only)" asks for `read:user`. "Sign in
  with private repos" asks for `repo read:user`. Public-only login still
  helps: it proves the user owns the account, skips the cooldown, and runs
  on the user's own API quota instead of the server's.
- **No sessions or database.** The token lives only on the job, as a pasted
  token does today, and is revoked when the job ends, so nothing long-lived
  is left behind. The only new state is the pending options keyed by
  `state`, kept in memory with a short TTL.
- **Security.** Use a random `state` bound to a cookie, PKCE `S256`, and an
  exact callback URL. The client secret comes from env and is never logged.
  The login is checked against the target username; signing in as A and
  generating B renders public data only, which the current code already
  enforces.
- **Optional by config.** The feature is off unless
  `GHGLANCE_OAUTH_CLIENT_ID` and `GHGLANCE_OAUTH_CLIENT_SECRET` are set. The
  manual token field stays as a fallback.
- **One-time setup.** Register an OAuth App under the account with callback
  `https://ghglance.sg.miti99.com/auth/callback` and put both values in
  Coolify.

## Facts the design rests on

- The web flow supports `state` and PKCE (`code_challenge`, `S256`). Codes
  expire after 10 minutes. GitHub allows 10 tokens created per hour per
  user, app and scope. ([docs][flow])
- OAuth App tokens are long-lived by default. GitHub App user tokens expire
  after 8 hours. A GitHub App only reaches the repos it is installed on.
  Organization OAuth policies can block OAuth Apps until an owner approves.
  ([docs][diff])
- `repo` grants read and write access to every repo the user can reach.
  There is no read-only private-repo scope for OAuth Apps. ([docs][scopes])
  That is why the design revokes the token after each job.

[flow]: https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps
[diff]: https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/differences-between-github-apps-and-oauth-apps
[scopes]: https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps

## Unresolved questions

- Should the private login also ask for `read:org`? Today's PAT
  instructions do not use it, and `-include-org-repos` works without it for
  repos the user administers.
- Whether `contributionsCollection` counts private contributions for a
  GitHub App user token without an install is not documented. It does not
  change the recommendation.
