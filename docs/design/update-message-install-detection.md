# Design: install-method detection for update messages

Status: draft
Author: Claude Code (for Patrick Dawkins)
Date: 2026-06-14

## Background

The CLI checks GitHub for a newer release and, if one exists, prints a message
after the command runs. The check runs in a background goroutine started in
`PersistentPreRun` (`commands/root.go:75`), and the message is printed in
`PersistentPostRun` via a non-blocking channel read (`commands/root.go:95`).

The check itself lives in `internal/update.go`:

- `CheckForUpdate` gates on `shouldCheckForUpdate`, throttles to
  `Updates.CheckInterval` (default 3600s) using `state.json`, fetches
  `releases/latest` from GitHub, and compares versions.
- `shouldCheckForUpdate` (`internal/update.go:61`) skips the check for dev
  builds, when `Updates.Check` is false, when `<PREFIX>UPDATES_CHECK=0`, in CI,
  and when stdout/stderr are not terminals.

The message is formatted by `printUpdateMessage` (`commands/root.go:202`). Today
it tailors the upgrade instruction in exactly one way: if the binary sits under
the Homebrew prefix (`isUnderHomebrew`, which shells out to `brew --prefix`), it
prints `brew update && brew upgrade <tap>`. Otherwise it prints a generic
`https://github.com/<repo>#upgrade` link.

## Problem

The message is shown identically regardless of how the CLI was installed, and it
is shown even when the user cannot act on it usefully:

1. For users who installed via a system package manager (`apt`, `yum`/`dnf`,
   `apk`), the OS updates the CLI on its normal schedule. Telling them to visit a
   GitHub page is noise, and following it would fight the package manager.
2. For Scoop, npm, and the bash installer, the generic GitHub link is less
   helpful than the one command that actually upgrades their installation.

## Goals

- Suppress the update message entirely when the CLI is managed by an
  auto-updating system package manager (`apt`, `yum`/`dnf`, `apk`).
- For manually-updated channels, print the exact upgrade command for that
  channel (Homebrew, Scoop, npm, bash installer).
- Fall back to today's generic GitHub link when the method is unknown.
- Make the notice substantially less naggy: interactive-only, throttled to once
  a week, shown before the command rather than buried after it.
- Offer one-tap, opt-in self-update for channels where the upgrade command is
  local and unprivileged, then re-run the original command on the new version.
- Keep detection off the synchronous command path (the network check already
  runs in the background goroutine) and avoid new latency on every invocation.
- Keep the design vendorizable — no hard-coded `upsun`-specific strings in Go.
- Provide an escape hatch to force or override detection.

## Non-goals

- Silent or unattended self-update. Updates are only ever performed after an
  explicit interactive prompt. `self:update` (the PHP command) stays disabled.
- Auto-running privileged or remote-code upgrades (`sudo`, `curl … | sh`). For
  those channels we print the command rather than executing it (see Phase 2).
- Changing the network-check throttle, the CI gate, or the TTY gate.
- Detecting manual `dpkg -i` / `rpm -i` installs that bypass a repo. These are
  treated as "package" (auto-updating); see Edge cases.

## Phasing

This grew from "tailor the message" into "opt-in self-update", so it ships in
phases:

- **Phase 1** — install-method detection, suppress for packages, tailor wording,
  weekly display throttle, show-before-via-cache. No process re-exec; low risk.
- **Phase 2** — interactive prompt + auto-update + re-exec for the unprivileged
  channels (Homebrew, Scoop, npm).
- **Phase 3 (optional)** — move the bash installer's `raw` method to a
  user-local path, which removes `sudo` and lets `script` installs join the
  one-tap auto-update set.

## Distribution channel inventory

Verified from `.goreleaser.yaml`, `installer.sh`, and `npm/wrapper/`.

| Channel | Where the running binary lives | Auto-updates? | Desired behavior |
|---|---|---|---|
| Homebrew (`brews:`) | `$(brew --prefix)/bin/<exe>` (symlink into `…/Cellar/`) | No | `brew update && brew upgrade <tap>` |
| Scoop (`scoops:`, Windows) | `…\scoop\apps\<exe>\current\<exe>.exe` (shim in `…\scoop\shims\`) | No | `scoop update <exe>` |
| npm (`npm/wrapper/`) | `…/node_modules/@upsun/cli-<suffix>/bin/<exe>` | No | `npm install -g <pkg>@latest` |
| apt / `.deb` (`nfpms:`) | `/usr/bin/<exe>` | Yes | suppress |
| yum·dnf / `.rpm` (`nfpms:`) | `/usr/bin/<exe>` | Yes | suppress |
| apk / `.apk` (`nfpms:`) | `/usr/bin/<exe>` | Yes | suppress |
| bash installer, raw method | Linux: `/usr/bin/<exe>`; macOS: `/usr/local/bin/<exe>`; or `$INSTALL_DIR` | No | re-run installer command |
| Unknown / built from source | anywhere | — | generic GitHub `#upgrade` link |

### The core ambiguity

On Linux the bash installer's `raw` method installs to `/usr/bin` (`installer.sh:37`),
which is the same location nfpm packages use. **Path alone cannot distinguish a
distro package (auto-updating, suppress) from a raw script install (manual,
show).** This is exactly the distinction the feature needs, so a path heuristic
is insufficient on Linux; we need a positive signal that a package manager owns
the binary.

The bash installer's `apt`/`yum`/`apk` methods install the distro package from
`repositories.upsun.com`, so those collapse into the package case and carry
whatever signal the package carries.

## Detection design

### A positive package marker (resolves the Linux ambiguity)

Have the nfpm packages install a small marker file that the binary can read at
runtime. nfpm `contents` can place a file at any absolute path:

```yaml
# .goreleaser.yaml, in each nfpms entry
contents:
  - src: packaging/install-source
    dst: /usr/share/<package_name>/install-source
    file_info: { mode: 0644 }
```

`packaging/install-source` contains the single word `package`. We do not need to
distinguish apt/yum/apk for suppression — all three auto-update — so one marker
value is enough, and we avoid splitting the single nfpm entry into three.

At runtime, presence of the marker (found relative to the resolved executable, or
at the known `/usr/share/<slug>/install-source` path) means "managed by a system
package manager → auto-updating". Its absence at `/usr/bin/<exe>` means the
binary got there some other way (raw installer) → not suppressed.

This keeps all packaging changes inside this repo's `.goreleaser.yaml`; the
Homebrew tap and Scoop manifests (in `upsun/homebrew-tap`) need no changes
because those channels are detected by path.

### Detection precedence

A new function `DetectInstallMethod(cnf) InstallMethod` resolves the method once
(memoized with `sync.Once`). Order, first match wins:

1. **Override.** `<PREFIX>INSTALL_METHOD` env var (e.g. `UPSUN_CLI_INSTALL_METHOD`),
   or an optional `wrapper.install_method` config key. Parsed into the enum;
   unknown values are ignored with a debug log. Lets CI, distro maintainers, and
   power users force behavior.
2. **Package marker.** Marker file present → `package`.
3. **npm.** Resolved executable path contains `node_modules` and the package
   scope (`@<vendor>/cli-` derived from config, or simply `node_modules`). → `npm`.
4. **Scoop** (Windows only). Path contains a `scoop{sep}` segment. → `scoop`.
5. **Homebrew.** Existing `isUnderHomebrew` logic (path under `brew --prefix`),
   plus a cheap pre-check for `/Cellar/` in the path to avoid the subprocess when
   obviously not Homebrew. → `homebrew`.
6. **Script.** Otherwise, if the binary is in a normal bin dir
   (`/usr/bin`, `/usr/local/bin`, `$INSTALL_DIR`-style) → `script`.
7. **Unknown.** Anything else (e.g. `go run`, source builds). → `unknown`.

Resolve symlinks with `filepath.EvalSymlinks(os.Executable())` before matching so
that shimmed/symlinked installs report their real location.

Because detection runs inside the background goroutine (`CheckForUpdate`) and at
message time — never on the synchronous hot path — shelling out to `brew --prefix`
remains acceptable. Memoization means it happens at most once per process.

### Enum

```go
type InstallMethod string

const (
    InstallUnknown  InstallMethod = ""
    InstallHomebrew InstallMethod = "homebrew"
    InstallScoop    InstallMethod = "scoop"
    InstallNpm      InstallMethod = "npm"
    InstallPackage  InstallMethod = "package" // apt / yum / dnf / apk
    InstallScript   InstallMethod = "script"  // bash installer, raw method
)

// AutoUpdating reports whether the host system updates the CLI on its own.
func (m InstallMethod) AutoUpdating() bool { return m == InstallPackage }
```

### Wiring into the check

In `shouldCheckForUpdate`, short-circuit when auto-updating so we skip both the
message and the network request:

```go
if DetectInstallMethod(cnf).AutoUpdating() {
    return false
}
```

`printUpdateMessage` switches on the method to choose the upgrade line. Since
`AutoUpdating` methods never reach the message, the switch only handles the
manual channels plus the unknown fallback (current behavior preserved).

## Behavior matrix

"Auto-updatable" means the upgrade command is local and unprivileged, so we may
offer to run it for the user (Phase 2). `script`/`unknown` only ever print a
hint.

| Method | Notice shown? | Upgrade command | Auto-updatable? |
|---|---|---|---|
| `package` | No (suppressed) | — | — (OS handles it) |
| `homebrew` | Yes | `brew update && brew upgrade <wrapper.homebrew_tap>` | Yes |
| `scoop` | Yes | `scoop update <application.executable>` | Yes |
| `npm` | Yes | `npm install -g <wrapper.npm_package>@latest` | Yes |
| `script` | Yes | `curl -fsSL <wrapper.installer_url> \| sh` | No (remote + maybe sudo)¹ |
| `unknown` | Yes | `follow the instructions at https://github.com/<repo>#upgrade` (today's text) | No |

¹ Becomes auto-updatable in Phase 3 if raw installs move to a user-local,
sudo-free path.

All upgrade strings are built from config fields, not literals, so vendor builds
get correct instructions.

## Reducing nagginess

The point of the notice is to be useful, not to interrupt. Four changes make it
quiet enough to keep on by default, plus an opt-in updater.

### Interactive-only

The passive one-line notice shows on any TTY (today's gate). The *prompt* and any
auto-update only happen when fully interactive: TTY **and** not `--yes` /
`--no-interaction`. Also skip plumbing commands (`completion`, `--version`) so we
never corrupt machine-readable output.

### Weekly display throttle

Decouple "how often we check the network" (hourly, `LastChecked`) from "how often
we tell the user" (weekly). Add a separate timestamp; set it whenever we show the
notice or prompt, regardless of whether the user accepts or declines, so we stay
quiet for a week either way.

### Show before the command, via a cache

Today the notice prints in `PersistentPostRun`, after the output, because the
background check may not have finished. Instead, cache the latest known version
in `state.json`. `PersistentPreRun` reads the cache and shows/prompts
immediately — no blocking — while the background goroutine refreshes the cache
for the *next* invocation. The notice therefore lags real availability by one
command, which is acceptable, and it appears before output (and is a prerequisite
for prompting, which cannot happen after the command has run).

State (`internal/state/state.go`) gains:

```go
Updates struct {
    LastChecked        int64  `json:"last_checked"`
    LastNotified       int64  `json:"last_notified,omitempty"`
    KnownLatestVersion string `json:"known_latest_version,omitempty"`
}
```

### Opt-in one-tap update + re-exec (Phase 2)

When fully interactive, an update is known, the weekly throttle allows it, and
the method is auto-updatable (Homebrew/Scoop/npm), prompt:

```
A new release of the Upsun CLI is available: v2.3.1 → v2.4.0
Update now? [Y/n]
```

- **Default.** Enter = yes (your "just hit enter" flow) — but *only* for the
  unprivileged channels. We never default-run a `sudo` or `curl … | sh` upgrade;
  for `script`/`unknown` we print the command and do not prompt to run it.
- **On accept.** Run the channel's upgrade command, streaming its output. On
  success, re-resolve the (possibly relocated) executable and re-run the original
  command on the new version:
  - Unix: `syscall.Exec(newBinary, os.Args, os.Environ())` — replaces the process
    image. Safe because we prompt in `PersistentPreRun`, before any command logic
    has run.
  - Windows: spawn the new binary with the original args, forward stdio, exit with
    its status (the pattern `npm/wrapper/bin/upsun.js` already uses).
  - On upgrade failure: print a warning and continue with the current version —
    never block the user's command.
- **On decline.** Set `LastNotified` and run the command normally; stay quiet for
  a week.

Because the prompt and update run in `PersistentPreRun`, and the legacy PHP layer
is already invoked with `<PREFIX>UPDATES_CHECK=0` (`internal/legacy/legacy.go:139`),
there is no double-notification when delegating to PHP commands.

### The bash installer's destination (Phase 3, optional)

The `raw` method installs to `/usr/bin` on Linux (`installer.sh:37`), which needs
`sudo` and collides with the package path (the source of the detection
ambiguity). Targeting a user-local dir instead (`$XDG_BIN_HOME`, else
`~/.local/bin`) would remove `sudo`, make `script` unambiguous from `package`
without relying on the marker, and let `script` installs join the one-tap update
set. Back-compat: existing `/usr/bin` installs stay where they are; PATH ordering
must be considered. Tracked separately from Phases 1–2.

## Config schema changes

Add to the `Wrapper` struct (`internal/config/schema.go:22`):

```go
Wrapper struct {
    HomebrewTap   string `yaml:"homebrew_tap,omitempty"`
    GitHubRepo    string `yaml:"github_repo,omitempty"`
    NpmPackage    string `yaml:"npm_package,omitempty"`    // e.g. "upsun"
    InstallerURL  string `yaml:"installer_url,omitempty"`  // e.g. "https://raw.githubusercontent.com/upsun/cli/main/installer.sh"
    InstallMethod string `yaml:"install_method,omitempty"` // optional forced override
}
```

Populate `npm_package` and `installer_url` in `internal/config/upsun-cli.yaml` and
`internal/config/platformsh-cli.yaml`. A channel with an empty config field
simply falls back to the generic GitHub link for that branch (graceful for vendor
builds that don't ship that channel).

## Code changes

Phase 1:

- `internal/update.go`
  - New `InstallMethod` type + `AutoUpdating`.
  - New `DetectInstallMethod(cnf)` (memoized) with the precedence above.
  - `shouldCheckForUpdate`: short-circuit on `AutoUpdating()`.
  - Cache the latest known version into state during the background check.
- `internal/state/state.go`
  - Add `LastNotified` and `KnownLatestVersion`.
- `commands/root.go`
  - Read the cached version in `PersistentPreRun`; show the tailored notice there
    (throttled weekly, interactive-only) instead of in `PersistentPostRun`.
  - `printUpdateMessage`: replace the Homebrew-only `if/else` with a `switch` on
    `DetectInstallMethod(cnf)`. Move/keep `isUnderHomebrew` as a detector helper.

Phase 2:

- New `internal/update` updater: `Upgrade(ctx, method, cnf)` runs the channel's
  command; `reexec(args)` does `syscall.Exec` (Unix) / spawn-and-forward (Windows).
- `commands/root.go` `PersistentPreRun`: when interactive + auto-updatable +
  update known + weekly-allowed, prompt; on accept, `Upgrade` then `reexec`.

## Packaging changes

- `.goreleaser.yaml`: add the `install-source` marker to both `nfpms` entries'
  `contents`.
- `packaging/install-source`: new one-line file containing `package`.
- `installer.sh`: no change required for suppression (its `apt`/`yum`/`apk`
  methods install the marked package). Optional follow-up: have the `raw` method
  export `UPSUN_CLI_INSTALL_METHOD=script` guidance in its success output, or
  write a `script` marker, so `script` detection is explicit rather than inferred.

## Edge cases

- **Manual `dpkg -i` / `rpm -i` of a downloaded package** still carries the
  marker, so it is treated as `package` and suppressed even though it will not
  auto-update from a repo. Acceptable: the user chose a package format and can
  re-run the same install, and the GitHub releases page is still discoverable.
- **`brew --prefix` unavailable / slow.** The `/Cellar/` pre-check avoids the
  subprocess in the common non-Homebrew case; if `brew` is missing we fall
  through to `script`/`unknown`, matching today's behavior.
- **npm global vs local, pnpm, nested installs.** Matching on `node_modules` in
  the resolved executable path covers flat, nested, and pnpm layouts (the same
  resolution `npm/wrapper/bin/upsun.js` relies on).
- **`$INSTALL_DIR` to a non-standard dir.** Reports `script` (no marker, not a
  known package path) — correct.
- **Windows non-Scoop (raw zip on PATH).** No marker, not under scoop → `unknown`
  → generic link. Acceptable until a Windows installer exists.

## Known limitations

- `state.json` has no file locking (`state.Save` is a bare write). Two CLI
  invocations running concurrently can lose each other's writes: the background
  `CheckForUpdate` of one process can overwrite the `LastNotified` just written
  by another process's `MarkNotified`, so the weekly throttle may occasionally
  let a notice through more than once. This races on a cosmetic field only, and
  the file was already written without locking before this change. Fixing it
  properly (advisory lock around load-modify-save) is out of scope for Phase 1.

## Testing plan

Table-driven tests in `internal/update_test.go` for `DetectInstallMethod`,
injecting the executable path, GOOS, env, and a marker-file probe via small
interfaces/fakes (avoid real `brew`/filesystem):

- marker present → `package`; `AutoUpdating()` true.
- `…/node_modules/@upsun/cli-linux-x64/bin/upsun` → `npm`.
- `…\scoop\apps\upsun\current\upsun.exe` (GOOS=windows) → `scoop`.
- brew-prefix path → `homebrew`.
- `/usr/bin/upsun` no marker → `script`.
- `<PREFIX>INSTALL_METHOD=homebrew` overrides a `/usr/bin` path.
- `go run` temp path → `unknown`.

For `shouldCheckForUpdate`: assert it returns false when the method is a package,
true otherwise (given the other gates pass). For `printUpdateMessage`: assert the
upgrade line per method, including empty-config fallbacks.

## Docs to update

- `README.md` `#upgrade` section — note that package-manager installs update via
  the OS and that the message is suppressed there.
- Upsun docs CLI admin page
  (`sites/upsun/src/administration/cli`) — document `<PREFIX>INSTALL_METHOD` and
  reconfirm `<PREFIX>UPDATES_CHECK=0`.
- `CLAUDE.md` "Update Checks" section — mention install-method detection.

## Resolved decisions

- `package` installs: total silence, no hint. The OS handles updates.
- Manual `dpkg -i`/`rpm -i`: treated as `package` (suppressed). Acceptable.
- npm upgrade command: global `npm install -g <pkg>@latest`.
- Ambiguous / no marker: fall back to `script`/`unknown`, advise re-running the
  installer (or the GitHub link).
- Prompt default: Enter = yes, but only for unprivileged channels
  (Homebrew/Scoop/npm). Never default-run `sudo`/`curl | sh`.

## Open questions

1. Phase 3: do we move the bash installer's `raw` destination to a user-local
   path? It simplifies detection and unlocks `script` auto-update, but changes
   long-standing behavior and PATH expectations.
2. Windows re-exec after update: spawn-and-forward is more complex than Unix
   `syscall.Exec`. Acceptable to ship Phase 2 for macOS/Linux first and print the
   command on Windows until the spawn path is built?
3. Should there be a config/env switch to disable just the *prompt* (keep the
   passive notice) for users who want to be told but never asked to act? Or is
   `<PREFIX>UPDATES_CHECK=0` (disables everything) enough?
