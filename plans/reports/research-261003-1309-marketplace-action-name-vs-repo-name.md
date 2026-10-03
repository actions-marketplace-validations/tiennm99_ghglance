# Can a Marketplace action name match its repo name?

Researched 2026-10-03 13:09 (Asia/Saigon).

## Answer

Yes. GitHub never checks the action's `name` against repository names,
including the action's own repo. The repo `tiennm99/ghstats` is not why
`ghstats` was rejected. The name collides with something else on GitHub.

## Official rule

From [Publishing actions in GitHub Marketplace](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/publish-in-github-marketplace),
the `name` in `action.yml`:

- must be unique;
- cannot match an action name already published on GitHub Marketplace;
- cannot match a GitHub user or organization, unless that user or
  organization owner is the one publishing;
- cannot match a GitHub Marketplace category;
- cannot be a reserved GitHub feature name.

None of these rules involve repositories. The owner exception covers only
user and organization names. For example, `tiennm99` could publish an action
called `tiennm99`, but nothing similar applies to repo names.

## Why `ghstats` conflicts

Public checks made on 2026-10-03 found no visible conflict:

- `gh api users/ghstats` returned 404, so there's no visible user or org.
- `github.com/marketplace/actions/ghstats` returned 404, so there's no visible listing.

GitHub still enforces the name, so it must be held by something the public
can't see. The likely holders are:

1. **A suspended, flagged or otherwise hidden account named `ghstats`.**
   The REST API returns 404 for these accounts, but the name stays reserved.
2. **A delisted or unpublished Marketplace action named `ghstats`.** It no
   longer has a public page but still holds the name.
3. **A reserved name** on GitHub's internal list.

This repo's own history supports this: `docs/deployment-guide.md` recorded
that "the bare `ghstats` listing was taken" before this session.

Other projects have hit the same error and solved it the same way, by
renaming the action. Examples:
[baseliner-action#9](https://github.com/baselinerhq/baseliner-action/pull/9),
[simulation-action#12](https://github.com/roarkhq/simulation-action/pull/12),
[ChatGPT-CodeReview#5](https://github.com/arepresas/ChatGPT-CodeReview/pull/5).

## Next steps

1. Pick a different `name`. `ghstats-cards` is the safest choice because it
   was used for this repo's listing before.
2. The listing name has no effect on how people call the action:
   `uses: tiennm99/ghstats@v1` keeps working because it comes from the repo
   path.
3. Rename in `action.yml`, update the README badge, the listing link and the
   deployment guide, then tag a new release and publish it from the release page.

## Unresolved questions

- Which of the three possible holders owns `ghstats` can't be found out
  from outside GitHub. GitHub Support could say whether it can be released.
