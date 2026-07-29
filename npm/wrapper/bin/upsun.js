#!/usr/bin/env node
// Resolves the platform-specific package installed via optionalDependencies,
// then execs the embedded binary, forwarding argv, stdio, and exit code.

const { spawnSync } = require("node:child_process");
const path = require("node:path");

// macOS ships a single universal binary, so both Apple Silicon and
// Intel resolve to the same "darwin" package.
const TARGETS = {
  "darwin:x64": { suffix: "darwin", binary: "upsun" },
  "darwin:arm64": { suffix: "darwin", binary: "upsun" },
  "linux:x64": { suffix: "linux-x64", binary: "upsun" },
  "linux:arm64": { suffix: "linux-arm64", binary: "upsun" },
  "win32:x64": { suffix: "win32-x64", binary: "upsun.exe" },
};

const target = TARGETS[`${process.platform}:${process.arch}`];
if (!target) {
  console.error(
    `upsun: no prebuilt binary for ${process.platform}-${process.arch}.`,
  );
  process.exit(1);
}

const pkgName = `@upsun/cli-${target.suffix}`;

let binary;
try {
  // require.resolve handles flat, nested, and pnpm-style installs.
  const pkgJsonPath = require.resolve(`${pkgName}/package.json`);
  binary = path.join(path.dirname(pkgJsonPath), "bin", target.binary);
} catch (err) {
  console.error(
    `upsun: platform package "${pkgName}" is not installed.\n` +
      `If you installed with --no-optional or --ignore-optional, reinstall without that flag.\n` +
      `Original error: ${err.message}`,
  );
  process.exit(1);
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`upsun: failed to exec ${binary}: ${result.error.message}`);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
}

process.exit(result.status ?? 1);
