# npm distribution

Tooling to ship the Upsun CLI as an npm package, so users can run
`npm install -g upsun` or `npx upsun`. Implements the
`optionalDependencies` pattern used by esbuild, swc, biome, turbo, and
others: a small wrapper package selects the right platform-specific
package at install time, so each user only downloads the binary that
matches their OS and CPU. No postinstall script, no runtime download.

## Packages

| Package                  | Contents                                |
| ------------------------ | --------------------------------------- |
| `upsun`                  | wrapper, with the four platforms below as `optionalDependencies` |
| `@upsun/cli-linux-x64`   | Linux amd64 binary                      |
| `@upsun/cli-linux-arm64` | Linux arm64 binary                      |
| `@upsun/cli-darwin`      | macOS universal binary (x64 + arm64)    |
| `@upsun/cli-win32-x64`   | Windows amd64 binary                    |

## Layout

```
npm/
├── wrapper/                 wrapper package source
│   ├── bin/upsun.js         shim that resolves the platform package and execs the binary
│   ├── package.json.tmpl    stamped with version at build time
│   └── README.md            shipped to the registry as the wrapper README
├── platform-template/       common template for all platform-specific packages
│   ├── package.json.tmpl    stamped per-target with name, version, os, cpu
│   └── README.md.tmpl
├── scripts/
│   ├── build.sh             assembles tarballs from GoReleaser archives
│   └── publish.sh           publishes tarballs in lockstep
└── dist/                    build output (npm pack tarballs); gitignored
```

## Build

```sh
make snapshot-no-nfpm   # or any goreleaser invocation that writes upsun_*.tar.gz/zip into dist/
make npm-pack           # reads dist/, writes npm/dist/*.tgz
```

The build script resolves the version from the GoReleaser archive
filenames. Override with `VERSION=...` if you need to.

## Publish

```sh
make npm-publish              # publish all five packages in lockstep
DRY_RUN=1 make npm-publish    # validate without publishing
NPM_TAG=next make npm-publish # for prereleases
```

The script publishes platform packages first, then the wrapper, so the
registry is never in a state where the wrapper points at platform
packages that don't yet exist.

Auth is via the standard npm mechanism: `~/.npmrc` with a token, or the
`actions/setup-node` action in CI populating one for you from
`NODE_AUTH_TOKEN`. The `--access public` flag is set so first-time
publishes of scoped packages do not get marked private.

## Versioning

Every npm release uses the same version as the corresponding GitHub
release tag. Platform packages and the wrapper are always published in
lockstep at the same version; the wrapper's `optionalDependencies` pin
exact versions, so a mismatched set will not resolve.

## Known limitations

- `npm install --no-optional` (or `--omit=optional`) skips the platform
  package, and the wrapper exits with a clear error pointing at the flag.
- `darwin-arm64` and `darwin-x64` share a single universal binary
  package. This roughly doubles the macOS install size relative to
  per-arch packages, but matches the artifact GoReleaser produces and
  keeps the package set smaller.
