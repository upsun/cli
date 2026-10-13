# Legacy PHP CLI

This directory contains the PHP layer of the Upsun CLI. It is built into a
phar and embedded in the Go wrapper, which runs it with its own PHP binary.
Commands not implemented in Go are passed to it.

It was merged in from the archived
[platformsh/legacy-cli](https://github.com/platformsh/legacy-cli) repository.
Changes are now made here.

See the [root README](../README.md) for installing, configuring and building
the CLI.

## Development

Install dependencies:

```sh
composer install
```

Run the PHP CLI from source:

```sh
./bin/platform
```

Run linters (php-cs-fixer and PHPStan) and unit tests:

```sh
make lint
make test
```

After changing services or commands, delete the cached container
(`make clean`).

To build the embedded phar, run `make single` from the repository root.

A Docker development environment is available: copy `.env-dist` to `.env`,
then run `docker-compose up -d` and `docker-compose exec cli bash`.
