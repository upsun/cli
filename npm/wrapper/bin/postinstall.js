#!/usr/bin/env node
// Prints a one-line hint after `npm install -g upsun` to surface the optional
// `upsun completion install` step. Silent for non-global installs (the wrapper
// as a transitive dependency), non-interactive contexts (CI, npx cache
// warmups), and when UPSUN_NO_COMPLETION_HINT is set. Never modifies any
// shell config.

if (!process.env.npm_config_global) {
  process.exit(0);
}
if (!process.stdout.isTTY) {
  process.exit(0);
}
if (process.env.CI || process.env.UPSUN_NO_COMPLETION_HINT) {
  process.exit(0);
}

console.log("To enable shell completion, run: upsun completion install");
