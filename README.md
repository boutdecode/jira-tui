# jira-tui

A TUI for Jira Cloud, built with [Charm](https://charm.land/) (Bubble Tea / Bubbles / Lip Gloss v2). Go 1.27+.

## Running

    make demo        # fake data, no network
    make run         # against your Jira instance
    ./jira-tui --transparent   # keep your terminal background (transparency, image…)

## Command-line usage

    jira-tui                              # ticket list
    jira-tui PAY-142                      # open the ticket detail directly
    jira-tui https://x.atlassian.net/browse/PAY-142   # same, with the URL
    jira-tui PAY-142 --output-json        # no UI: the ticket as JSON on standard output

The reference is a key (case-insensitive) or a Jira URL (`/browse/PAY-142`, `?selectedIssue=PAY-142`).
Options can be placed before or after the reference. `--output-json` requires a reference and is never
interactive (no setup form).

    jira-tui PAY-142 --output-json | jq -r .status.name

The JSON (indented, UTF-8) contains: `key`, `id`, `url`, `summary`, `description` (plain text), `type`,
`status` (`name`, `category`), `priority`, `assignee`, `reporter`, `labels`, `components`, `created`, `updated`,
`original_estimate_seconds`, `comments` (`author`, `created`, `visibility` = `client` / `internal` / `unknown`, `body`),
and, if Tempo is configured, `time_spent_seconds` and `worklogs` (`null` otherwise). A missing value is `null`.
If Tempo or the comments cannot be read, the ticket is still exported and
`worklogs_error` / `comments_error` explains why (a warning is also printed on stderr).

Exit codes: `0` ok · `1` error (network, authentication, config, ticket not found) · `2` usage
(invalid or missing reference, unknown option). Errors are written to stderr, never to stdout.

## Language

The interface (screens, shortcuts, messages, `--help`, demo data) is available in **French** and **English**.
The language follows the system locale: `LC_ALL`, then `LC_MESSAGES`, `LANGUAGE` and `LANG` (the first one that is
set and is not `C`/`POSIX` decides). A French locale (`fr_FR.UTF-8`, `fr_CA`…) gives French, anything else gives English.
To force a language whatever the locale, set `JIRA_TUI_LANG=fr` or `JIRA_TUI_LANG=en`:

    JIRA_TUI_LANG=en jira-tui

Inputs are understood in both languages (`hier` / `yesterday`, `1j` / `1d`). The JSON output of `--output-json`
and the config file keys are the same in both languages.

## Configuration

On first launch, with no environment variables and no config file, the app shows a form
(URL, email, token). The credentials are checked against Jira and then saved to
`~/.config/jira-tui/config.yaml` (mode `0600`). Outside an interactive terminal, it stops and lists the missing values.

Environment variables (higher priority) or `~/.config/jira-tui/config.yaml`:

    url: https://xxx.atlassian.net
    email: you@example.com
    token: <Atlassian API token>
    default_jql: assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC
    filters:
      - {name: My bugs, jql: "assignee = currentUser() AND type = Bug"}

Env: `JIRA_URL`, `JIRA_EMAIL`, `JIRA_TOKEN`. Token: https://id.atlassian.com/manage-profile/security/api-tokens

## Time tracking (Tempo)

If you use Tempo, the Detail screen shows the logged time ("Temps passé" section, total and estimate)
and `t` lets you add some.

1. Create a **Tempo** token (separate from the Atlassian token): Tempo → Settings → *API integration* → *New token*
   (permissions: view and manage worklogs).
2. Add it to the config (or `TEMPO_TOKEN`):

        tempo_token: <Tempo token>
        tempo_url: https://api.tempo.io/4   # optional (other region: api.eu.tempo.io/4…)
        hours_per_day: 8                    # optional, for "1d"

Without `tempo_token`, the app works as before. In the `t` dialog: **duration** (`1h30`, `45m`, `1.5h`, `2d`, `1d 2h`;
a bare number = minutes), **date** (empty = today, `hier`, `04/11`, `2026-11-04`) and an optional description.
Only adding is supported: to fix or delete a worklog, use Tempo.

## Client / internal comments (Jira Service Management)

`c` opens the "Nouveau commentaire" field, **visible to the customer by default**. `tab` switches between
`Client` (blue) and `Interne` (yellow, not visible to the customer); the label and the border color
always show the selected visibility. Each new comment starts again as `Client`.
Existing comments show `[client]` or `[interne]` (nothing if Jira does not say,
for example outside Service Management).

## Jira automations (manual rules)

`x` (from the list, the kanban or the detail) lists the automations with a **"Manual" trigger** that apply to the
selected ticket and lets you run them. A confirmation is always requested (a rule can modify the ticket
or write to the customer). Rules that ask for input ("Get input from users") show a form:
`TEXT` and `NUMBER` types are supported; a rule with another input type (user, list…) is flagged and
never run — use Jira for that one.

Jira answers "started", not "finished": the rule then runs on the Jira side, and the app reloads the ticket 3 s later.

Prerequisite: the Jira token must be a **classic** (unscoped) API token; the Automation API accepts no other.
If the automatic search does not work on your instance, the error message is displayed and you can
declare your rules by hand (the id is in the URL of the rule editor):

    cloud_id: 182b9218-…          # optional, detected automatically
    automations:                  # optional: always offered in addition to the search
      - {name: "Escalader au support", id: "0192f2c3-…"}

## Shortcuts

Three tabs, as in the mockup: `1 Liste`, `2 Kanban`, `3 Détail` (last opened ticket).

| View | Keys |
|---|---|
| Global | `1` `2` `3` / `tab` tabs · `s` change status · `o` open in Jira · `b` choose a board · `?` help · `q` quit |
| List | `↑/↓` · `enter` open · `/` filter (text, live; `esc` clears) · `m` assigned me/everyone · `O` open/all · `ctrl+j` JQL query · `f` config filters · `r` reload |
| Kanban | `←/→` column · `↑/↓` card · `enter` open · `H/L` move the ticket (transition) |
| Detail | `↑/↓` scroll · `c` comment (`tab` client/internal, `ctrl+s` sends, `esc` cancels) · `t` add time (Tempo) · `a` assign to me · `u` unassign · `esc` back |

The "assigned: me" / "status: open" chips build the JQL
(`assignee = currentUser() AND resolution = Unresolved`). A JQL typed with `ctrl+j`
(or a config filter) replaces them until the next `m` / `O`.

If Jira answers 429, the request is retried up to 3 times (`Retry-After` header, otherwise 1 s, 2 s, 4 s).
