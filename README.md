# ghglance

> Generate SVG cards summarizing a GitHub user's profile — written in Go.

[![Marketplace](https://img.shields.io/badge/Marketplace-ghglance-2f81f7?logo=github)](https://github.com/marketplace/actions/ghglance)
[![Release](https://img.shields.io/github/v/release/tiennm99/ghglance?color=blue)](https://github.com/tiennm99/ghglance/releases/latest)
[![License](https://img.shields.io/github/license/tiennm99/ghglance?color=green)](./LICENSE)

`ghglance` is a single-binary CLI (and a GitHub Action wrapping it) that fetches
data for a GitHub user and writes a themed set of SVGs you can embed in your
profile README. The same binary can also [run as a web UI](#run-the-web-ui)
where anyone submits a username and gets the cards back.

Marketplace listing: **[ghglance](https://github.com/marketplace/actions/ghglance)** · Source: [`tiennm99/ghglance`](https://github.com/tiennm99/ghglance)

Cards rendered:

| # | Card | What it shows |
| --- | --- | --- |
| 0 | Profile details | Login (Name) title + Octicon-labelled rows for company, location, link, join date (with age), followers/following, repo count |
| 1 | Repos per language | Donut + legend: how many owned non-fork repos use each language as primary |
| 2 | Most commit language (last year) | Donut + legend: last-year commits byte-weighted across each repo's language breakdown |
| 3 | Stats | Star, commit (lifetime + last-year), PR, issue, PR-review, contributed-to totals |
| 4 | Productive time (last year) | 24-hour bar chart with axes, title includes `UTC±N.NN` |
| 5 | Productive weekday (last year) | 7-bar day-of-week chart, peak day highlighted |
| 6 | Contributions (last year) | Smooth monthly area chart, Y-axis mirrored both sides, `mm/yy` labels |
| 7 | Contributions heatmap | Classic 7×53 calendar grid with theme-derived intensity ramp and legend |
| 8 | Top starred repos | Top 7 owned non-fork repos by ⭐, language dot + proportional bar |
| 9 | Streak | Current streak, longest streak, active days / total days with date ranges |
| 10 | **Most commit language (all time)** | Same as #2 but over lifetime commits |
| 11 | **Productive time (all time)** | Same as #4 but over lifetime commits |
| 12 | **Productive weekday (all time)** | Same as #5 but over lifetime commits |
| 13 | **Contributions (all time)** | Area chart across every active year, auto-thinned x-axis labels |
| 14 | **Contributions by year** | One bar per active year, peak year highlighted |
| 15 | **Records (all time)** | Six personal-best rows: peak day, peak month, first contribution, lifetime active days, account age, languages used |

## Preview — dracula theme

Live render against the author's profile, committed by [`.github/workflows/demo.yml`](./.github/workflows/demo.yml) on every push to `main`. Rendered with `start_of_week: monday` so the heatmap rows and weekday bars start on Mon, and `include_org_repos` on so repos under the author's orgs count toward the repo and language totals. Other 64 themes in the [**demo gallery**](./demo).

<div align="center">

<table>
<tr><td><img src="./demo/dracula/profile-details.svg" alt="profile-details" /></td><td><img src="./demo/dracula/stats.svg" alt="stats" /></td></tr>
<tr><td><img src="./demo/dracula/repos-per-language.svg" alt="repos-per-language" /></td><td><img src="./demo/dracula/top-starred-repos.svg" alt="top-starred-repos" /></td></tr>
<tr><td><img src="./demo/dracula/contributions-by-year.svg" alt="contributions-by-year" /></td><td><img src="./demo/dracula/contributions-heatmap.svg" alt="contributions-heatmap" /></td></tr>
<tr><td><img src="./demo/dracula/records.svg" alt="records" /></td><td><img src="./demo/dracula/streak.svg" alt="streak" /></td></tr>
<tr><th>Last year</th><th>All time</th></tr>
<tr><td><img src="./demo/dracula/most-commit-language.svg" alt="most-commit-language" /></td><td><img src="./demo/dracula/most-commit-language-all-time.svg" alt="most-commit-language-all-time" /></td></tr>
<tr><td><img src="./demo/dracula/contributions.svg" alt="contributions" /></td><td><img src="./demo/dracula/contributions-all-time.svg" alt="contributions-all-time" /></td></tr>
<tr><td><img src="./demo/dracula/productive-weekday.svg" alt="productive-weekday" /></td><td><img src="./demo/dracula/productive-weekday-all-time.svg" alt="productive-weekday-all-time" /></td></tr>
<tr><td><img src="./demo/dracula/productive-time.svg" alt="productive-time" /></td><td><img src="./demo/dracula/productive-time-all-time.svg" alt="productive-time-all-time" /></td></tr>
</table>

</div>

## In the wild

- [**tiennm99/tiennm99**](https://github.com/tiennm99/tiennm99) — author's profile README, refreshed daily via `tiennm99/ghglance@v1`. Two-per-row layout, dracula theme.

## Use as a GitHub Action (recommended)

Drop this in `.github/workflows/ghglance.yml` in your **profile repo** (the one
named after your username):

```yaml
name: ghglance

on:
  schedule:
    - cron: "0 0 * * *" # daily
  workflow_dispatch:

permissions:
  contents: write

jobs:
  cards:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: tiennm99/ghglance@v1
        with:
          user: ${{ github.repository_owner }}
          token: ${{ secrets.GHGLANCE_TOKEN }}   # classic PAT with read:user + repo
          themes: dracula,github_dark,tokyonight
          tz: Asia/Saigon
          include_forks: "true"
          include_private: "true"
          include_org_repos: "false"  # "true" also counts org repos you administer (token needs read:org)
          commit_changes: "true"
```

Then embed the cards in your `README.md`:

```md
![profile](./output/dracula/profile-details.svg)
![repos-per-language](./output/dracula/repos-per-language.svg)
![most-commit-language](./output/dracula/most-commit-language.svg)
![stats](./output/dracula/stats.svg)
![productive-time](./output/dracula/productive-time.svg)
![productive-weekday](./output/dracula/productive-weekday.svg)
![contributions](./output/dracula/contributions.svg)
![contributions-heatmap](./output/dracula/contributions-heatmap.svg)
![top-starred-repos](./output/dracula/top-starred-repos.svg)
![streak](./output/dracula/streak.svg)
![most-commit-language-all-time](./output/dracula/most-commit-language-all-time.svg)
![productive-time-all-time](./output/dracula/productive-time-all-time.svg)
![productive-weekday-all-time](./output/dracula/productive-weekday-all-time.svg)
![contributions-all-time](./output/dracula/contributions-all-time.svg)
![contributions-by-year](./output/dracula/contributions-by-year.svg)
![records](./output/dracula/records.svg)
```

### Action inputs

| Input              | Default                          | Description                                                             |
| ------------------ | -------------------------------- | ----------------------------------------------------------------------- |
| `user`             | —                                | GitHub username (required)                                              |
| `token`            | `${{ github.token }}`            | PAT with `read:user` + `repo` for private repo stats                    |
| `out`              | `output`                         | Output directory                                                        |
| `themes`           | `dracula`                        | Comma-separated theme ids, or `all`                                     |
| `tz`               | `UTC`                            | IANA tz for the productive-time card (e.g. `Asia/Saigon`)               |
| `start_of_week`    | `sunday`                         | First day of week for heatmap rows and weekday bars (`sunday`…`saturday`) |
| `top_repos`        | `0`                              | Optional cap on seed repos probed for commit history (`0` = unlimited)  |
| `commits_per_repo` | `500`                            | Max commits sampled per repo, `0` = every commit (covers last-year and all-time aggregates) |
| `include_forks`    | `true`                           | Include forked repos in stats and commit probing                        |
| `include_private`  | `true`                           | Include private repos (requires PAT with `repo` scope; silently no-op otherwise) |
| `include_org_repos`| `false`                          | Count org-owned repos you administer toward stars, repo count, languages, top-starred (needs `read:org`) |
| `commit_changes`   | `false`                          | Commit generated cards back to the repo                                 |
| `commit_message`   | `chore: update ghglance cards`    | Commit message                                                          |
| `commit_branch`    | *(current ref)*                  | Target branch for auto-commit                                           |
| `author_name`      | `github-actions[bot]`            | Commit author                                                           |
| `author_email`     | `…@users.noreply.github.com`     | Commit email                                                            |

## Use as a CLI

```sh
go install github.com/tiennm99/ghglance@latest
```

Or build from source:

```sh
git clone https://github.com/tiennm99/ghglance
cd ghglance
go build -o ghglance .
```

Then:

```sh
export GITHUB_TOKEN=ghp_xxx
ghglance -user tiennm99 -themes dracula,github_dark -tz Asia/Saigon -out output
```

Add `-include-org-repos` to also count org-owned repos you administer:

```sh
ghglance -user tiennm99 -themes dracula -include-org-repos -out output
```

| Flag                | Default         | Description                                                            |
| ------------------- | --------------- | ---------------------------------------------------------------------- |
| `-user`             | *(required)*    | GitHub username                                                        |
| `-token`            | `$GITHUB_TOKEN` | Personal access token                                                  |
| `-out`              | `output`        | Output directory (`<out>/<theme>/…svg`)                                |
| `-themes`           | `dracula`       | Comma-separated theme ids, or `all`                                    |
| `-tz`               | `Local`         | IANA timezone for productive-time cards                                |
| `-start-of-week`    | `sunday`        | First day of week for heatmap rows and weekday bars (`sunday`…`saturday`) |
| `-top-repos`        | `0`             | Optional cap on seed repos probed (`0` = unlimited)                    |
| `-commits-per-repo` | `500`           | Max commits sampled per repo, `0` = every commit                       |
| `-include-forks`    | `true`          | Include forked repos in the stats                                      |
| `-include-private`  | `true`          | Include private repos (requires `repo` PAT scope; silently no-op otherwise) |
| `-include-org-repos`| `false`         | Count org-owned repos you administer toward stars, repo count, languages, top-starred |
| `-timeout`          | `30m`           | Overall fetch deadline (per generation job under `-serve`), `0` = no limit |
| `-list-themes`      |                 | Print available theme ids and exit                                     |
| `-serve`            |                 | Run the [web UI](#run-the-web-ui) on this address (e.g. `:8080`) instead of generating once |
| `-data-dir`         | `data`          | Web UI only: directory holding generated cards                         |
| `-cooldown`         | `6h`            | Web UI only: minimum age of a user's cards before a token-less submission regenerates them |
| `-retention`        | `24h`           | Web UI only: delete a user's cards this long after they were generated, `0` = keep forever |
| `-workers`          | `2`             | Web UI only: concurrent generation jobs                                |

The five web UI flags are server-only: the Action (`action.yml`,
`entrypoint.sh`) does not expose them.

## Run the web UI

`-serve` turns the binary into a small web app: a form takes a GitHub
username plus options, a background job renders all sixteen cards in every
theme, and `/u/<username>` shows them again with a theme picker and
copyable embed URLs. Cards are stored on disk and survive restarts.

```sh
export GITHUB_TOKEN=ghp_xxx
ghglance -serve :8080 -data-dir data -retention 24h
# open http://localhost:8080
```

| Path | Serves |
| --- | --- |
| `/` | The submission form |
| `/u/<user>` | The user's cards (`?theme=<id>` picks the theme), or job progress while one runs |
| `/u/<user>/<theme>/<card>.svg` | One card, embeddable in a README |
| `/u/<user>/status` | Job status as JSON, polled by the progress page |
| `/healthz` | Liveness probe |

How submissions are handled:

- **Server token.** Submissions without a token use the server's
  `GITHUB_TOKEN`, with private repos and org repos forced off. That alone
  does not hide private work: GitHub counts every private contribution a
  token can see in the totals and the calendar. So the server token must be
  public-only (a classic PAT with just `read:user`, or a fine-grained token
  with public repositories only); a token with `repo` scope, or one that can
  list any private repository, is refused for token-less jobs and logged at
  startup. The token owner's own username is refused without a token too.
- **Submitter's token.** An optional token in the form is used for that one
  job, then dropped: never logged, never written to disk. When it belongs to
  the username being generated, private repos count by default and the
  cooldown is skipped. A token that belongs to someone else renders public
  data only, does not skip the cooldown, and is refused outright if it can
  read private repositories. The cards it renders are public on the site
  like any other.
  A "Create a token on GitHub" button beside the field opens GitHub's
  new-token page with a classic token's `repo` and `read:user` scopes
  pre-ticked.
- **Failures.** A job that fails or times out at any fetch stage publishes
  nothing, so an earlier complete set stays in place.
- **Cooldown.** Without a token, cards younger than `-cooldown` are shown
  instead of regenerated.
- **Retention.** Cards are deleted `-retention` (default `24h`) after they
  were generated, checked at startup and hourly. The user's page then
  offers a fresh generation, and embedded card URLs return 404 until
  someone regenerates them.
- **Limits.** One queued or running job per user, `-workers` jobs at once,
  `-timeout` per job, and five submissions per client followed by one
  every two minutes. A client is an IPv4 address or an IPv6 /64. Behind a
  reverse proxy on a private or loopback address, the client address comes
  from the last `X-Forwarded-For` hop.

Each user takes about 9 MB on disk for an active profile (16 cards × every theme).

### Deploy with Docker Compose or Coolify

[`compose.yml`](./compose.yml) builds the repo's `Dockerfile`, runs
`ghglance -serve :8080 -data-dir /data`, keeps cards in the `ghglance-data`
volume, and health-checks `/healthz`. It publishes no host port: Coolify's
proxy routes the domain it generates for `SERVICE_FQDN_GHGLANCE_8080` to
port 8080 in the container.

In Coolify:

1. Create a resource from this Git repository with the **Docker Compose**
   build pack and compose file `/compose.yml`.
2. Set `GHGLANCE_GITHUB_TOKEN` (see [`.env.example`](./.env.example)) to a
   public-only token: a classic PAT with only `read:user`, never `repo`.
   `compose.yml` requires it and passes it to the container as
   `GITHUB_TOKEN`. The distinct name keeps a `GITHUB_TOKEN` exported in your
   shell from silently replacing it during `docker compose up`.
3. Keep the generated domain or set your own on the `ghglance` service, then
   deploy.

On a plain Docker host, copy `.env.example` to `.env`, fill in the token,
add a `ports: ["8080:8080"]` entry to the service, and run (`.dockerignore`
keeps `.env` and `data/` out of the image context):

```sh
docker compose up -d --build
```

The Action image is unchanged: `compose.yml` overrides the entrypoint, so
the Action still runs `entrypoint.sh`.

## How attribution works

**Repo sampling** uses a seed list built from `contributionsCollection.commitContributionsByRepository`, unioned across every active contribution year. This catches every repo you've committed in — not just your top-starred ones. Each year is queried a quarter at a time: the API caps that field at 100 repos per query and drops the rest without saying so, which a prolific year hits easily. A quarter that still comes back at the cap is re-asked month by month to recover the tail.

**Which repos count where.** The commit-driven cards (most-commit-language, productive time, productive weekday, and everything derived from the contribution calendar) cover repos in *any* namespace you committed to — your own, your orgs', and upstream repos you sent PRs to. The repo-driven cards (stars, repo count, repos-per-language, top-starred) look only at repos you own. Set `include_org_repos` / `-include-org-repos` to also count org-owned repos where your permission is `ADMIN`; org repos you merely have read or write access to are never counted.

**Commit-to-language** is byte-weighted: each commit credits every language in the repo, proportional to linguist's byte share. A commit to a 60% Go / 40% Python repo adds 0.6 to Go and 0.4 to Python, regardless of which file was touched. Caveats:

- Linguist excludes prose (Markdown, AsciiDoc, reST) from byte counts, so heavily-Markdown repos skew toward whatever small code fraction linguist did detect.
- For per-file accuracy, a future `-accurate-languages` mode is planned (per-commit REST + go-enry).

**Cost per run** (current defaults, typical user):
- ~1 profile query + ~4 queries per active year (+3 for any quarter that saturates) + ~50 commit-history pages ≈ **80-100 GraphQL calls**.
- Zero REST calls. Well under the 5000 points/hr budget.

## Themes

Run `ghglance -list-themes` for the full list (65 themes ported from
github-profile-summary-cards). Built-ins include `default`, `dark`, `dracula`,
`github`, `github_dark`, `tokyonight`, `onedark`, `nord_dark`, `nord_bright`,
`gruvbox`, `radical`, `synthwave`, `monokai`, `solarized`, `solarized_dark`,
`transparent`, and more. Preview every one against real profile data in the
[demo gallery](./demo).

## Output

```
output/
  dracula/
    profile-details.svg
    repos-per-language.svg
    most-commit-language.svg
    stats.svg
    productive-time.svg
    productive-weekday.svg
    contributions.svg
    contributions-heatmap.svg
    top-starred-repos.svg
    streak.svg
    most-commit-language-all-time.svg
    productive-time-all-time.svg
    productive-weekday-all-time.svg
    contributions-all-time.svg
    contributions-by-year.svg
    records.svg
```

`output/` is entirely gitignored — it's regenerated on each run. For a
reference render of every card in every theme, see the CI-built
[`demo/`](./demo) gallery instead.

## Tokens & permissions

The default `${{ github.token }}` can read public user data but will not see
your private-repo commits. For accurate stats, create a **classic** personal
access token with `read:user` and `repo`, save it as a repo secret (e.g.
`GHGLANCE_TOKEN`), and pass it via the `token` input. `include_private`
defaults to `true` so those commits are counted automatically once the token
has `repo` scope; pass `include_private: "false"` if you want to keep private
work out of the rendered cards even when the token can see it.

Enabling `include_org_repos` additionally needs `read:org` on the token, and
SSO authorization for any org that enforces it — otherwise those repos stay
invisible and the input silently changes nothing.

## Credits & inspiration

- [**github-profile-summary-cards**](https://github.com/vn7n24fzkq/github-profile-summary-cards) by [@vn7n24fzkq](https://github.com/vn7n24fzkq) — card layout, chart styles, theme palette, Octicon selection, and output structure.

## License

Apache-2.0 — see [LICENSE](LICENSE).
