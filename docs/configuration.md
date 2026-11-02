# Configuration

You can override configuration in the user config file,
`~/.upsun-cli/config.yaml`. The available keys are in
[legacy/config-defaults.yaml](../legacy/config-defaults.yaml).

## Environment variables

- `UPSUN_CLI_TOKEN`: an API token, for non-interactive use such as CI. An API
  token can act as the account that created it, so use a separate machine
  account to limit its access. Interactively, prefer `upsun auth:api-token-login`.
- `UPSUN_CLI_DEBUG=1`: enable debug output. This can print HTTP request details,
  including access tokens.
- `UPSUN_CLI_DEFAULT_TIMEOUT`: the timeout in seconds for most API requests
  (default 30).
- `UPSUN_CLI_DISABLE_CACHE=1`: disable caching. To clear the cache once, run
  `upsun clear-cache`.
- `UPSUN_CLI_HOME`: override the home directory, which contains `.upsun-cli`.
- `UPSUN_CLI_NO_INTERACTION=1`: disable interaction, like `--no-interaction`.
  This skips confirmation questions.
- `UPSUN_CLI_SESSION_ID`: switch user session (default `default`). See also
  `upsun session:switch`.
- `UPSUN_CLI_AUTO_LOAD_SSH_CERT=0`: disable automatically loading an SSH
  certificate when running login or SSH commands.
- `UPSUN_CLI_SHELL_CONFIG_FILE`: the shell config file that `self:install`
  writes to (an absolute path). Set it to an empty string to skip writing one.
- `UPSUN_CLI_REPORT_DEPRECATIONS=1`: show PHP deprecation notices in debug mode
  (`-vvv`).
- `NO_COLOR=1` or `CLICOLOR_FORCE=0`/`1`: turn colors off or force them on.
- `http_proxy` or `https_proxy`: use an HTTP proxy.

The `UPSUN_CLI_` prefix and the `.upsun-cli` directory come from the CLI's
embedded config, so other builds of the CLI may use different ones.
