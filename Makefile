PHP_VERSION = 8.4.26

.DEFAULT_GOAL := help

# Delete a target if its recipe fails, e.g. a partial download.
.DELETE_ON_ERROR:

GOOS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
GOARCH := $(shell uname -m)
ifeq ($(GOARCH), x86_64)
	GOARCH=amd64
endif
ifeq ($(GOARCH), aarch64)
	GOARCH=arm64
endif

FLAVOR ?= upsun

ifeq ($(GOOS), darwin)
	GORELEASER_ID = $(FLAVOR)-macos
else
	GORELEASER_ID = $(FLAVOR)
endif

# Tooling versions
GORELEASER_VERSION=v2.12.0
REPOGEN_VERSION=v1.0.4

# PHP binaries are downloaded from cli-php-builds releases.
# See: https://github.com/upsun/cli-php-builds
PHP_BUILDS_REPO = upsun/cli-php-builds
PHP_RELEASE_URL = https://github.com/$(PHP_BUILDS_REPO)/releases/download/php-$(PHP_VERSION)

# Files embedded in the Go binary.
ARCHIVES = internal/legacy/archives
PHP_UNIX_BINARIES = $(foreach p,linux_amd64 linux_arm64 darwin_amd64 darwin_arm64,$(ARCHIVES)/php_$(p))
LEGACY_INDEXES = $(ARCHIVES)/commands-upsun.json $(ARCHIVES)/commands-platformsh.json

# Files that the legacy CLI phar is built from.
LEGACY_SOURCES := $(shell find legacy/bin legacy/src legacy/config legacy/resources -type f -not -path 'legacy/config/cache/*') \
	$(wildcard legacy/*.yaml legacy/*.tmpl.rc) legacy/constants.php legacy/box.json legacy/composer.lock

.PHONY: help
help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } \
		/^##@ / { printf "\n%s:\n", substr($$0, 5) } \
		/^[a-zA-Z0-9_-]+:.*## / { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Targets with a "##" description are listed by `make help`. Those automated in CI workflows are not.

##@ Build

.PHONY: single
single: goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php ## Build a single binary for the current platform, in dist/
	PHP_VERSION=$(PHP_VERSION) goreleaser build --single-target --id=$(GORELEASER_ID) --snapshot --clean

.PHONY: snapshot
snapshot: goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php $(ARCHIVES)/cacert.pem ## Build a snapshot release (needs signing keys)
ifndef RSA_SIGNING_KEY_FILE
	$(error RSA_SIGNING_KEY_FILE is not set. Set it to the path of your RSA private key for APK signing, or use 'make snapshot-no-nfpm' to skip packaging.)
endif
ifndef GPG_SIGNING_KEY_FILE
	$(error GPG_SIGNING_KEY_FILE is not set. Set it to the path of your GPG private key for RPM signing, or use 'make snapshot-no-nfpm' to skip packaging.)
endif
	PHP_VERSION=$(PHP_VERSION) goreleaser release --snapshot --clean --skip=publish,announce

.PHONY: snapshot-no-nfpm
snapshot-no-nfpm: goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php ## Build a snapshot release without packages
	PHP_VERSION=$(PHP_VERSION) goreleaser release --snapshot --clean --skip=publish,announce,nfpm

.PHONY: vendor-snapshot
vendor-snapshot: check-vendor .goreleaser.vendor.yaml goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) $(ARCHIVES)/commands-vendor.json php ## Build a vendor CLI snapshot (needs VENDOR_NAME, VENDOR_BINARY)
	PHP_VERSION=$(PHP_VERSION) VENDOR_BINARY="$(VENDOR_BINARY)" VENDOR_NAME="$(VENDOR_NAME)" goreleaser build --snapshot --clean --config=.goreleaser.vendor.yaml

.PHONY: phar
phar: $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) ## Build the legacy CLI phar and its command indexes

.PHONY: php
php: $(ARCHIVES)/php_$(GOOS)_$(GOARCH) ## Download the PHP binary for the current platform

# Download PHP binaries for all platforms.
.PHONY: download-php
download-php: $(PHP_UNIX_BINARIES) $(ARCHIVES)/php_windows.exe $(ARCHIVES)/cacert.pem

# Build the legacy CLI phar from the legacy/ subdirectory.
# The self:build command handles composer --no-dev rebuild, Box installation, and cache warming.
# The vendor directory is order-only, because self:build and composer-dev both rewrite it.
$(ARCHIVES)/platform.phar: $(LEGACY_SOURCES) | legacy/vendor/autoload.php
	mkdir -p $(ARCHIVES)
	cd legacy && php bin/platform self:build --no-interaction --output=../$@

# Index the legacy CLI's commands for each embedded config, so the Go layer can resolve abbreviations of its own
# commands and complete command and option names. HOME is isolated from user config. The version is set, as by the Go
# wrapper, so that the CLI does not try to read it from Git. Usage: $(call legacy-index,phar,config-file)
define legacy-index
	tmp=$$(mktemp -d) && \
	prefix=$$(go run ./scripts/env-prefix $(2)) && \
	env HOME=$$tmp CLI_CONFIG_FILE=$(abspath $(2)) "$${prefix}NO_LEGACY_WARNING=1" "$${prefix}APPLICATION_VERSION=0.0.0" \
		php $(1) list --all --format=json --no-interaction < /dev/null > $$tmp/commands.json && \
	mv $$tmp/commands.json $@; \
	status=$$?; rm -rf $$tmp; exit $$status
endef

$(ARCHIVES)/commands-%.json: $(ARCHIVES)/platform.phar internal/config/%-cli.yaml
	$(call legacy-index,$<,internal/config/$*-cli.yaml)

$(ARCHIVES)/commands-vendor.json: $(ARCHIVES)/platform.phar internal/config/embedded-config.yaml
	$(call legacy-index,$<,internal/config/embedded-config.yaml)

legacy/vendor/autoload.php:
	cd legacy && composer install --no-interaction

# Records PHP_VERSION, so that the PHP binaries are downloaded again when it changes.
$(ARCHIVES)/php-version: FORCE
	@mkdir -p $(ARCHIVES)
	@echo $(PHP_VERSION) | cmp -s - $@ || echo $(PHP_VERSION) > $@

$(PHP_UNIX_BINARIES): $(ARCHIVES)/php_%: $(ARCHIVES)/php-version
	curl -fSL "$(PHP_RELEASE_URL)/php-$(PHP_VERSION)-$(subst _,-,$*)" -o $@
	chmod +x $@

$(ARCHIVES)/php_windows.exe: $(ARCHIVES)/php-version
	curl -fSL "$(PHP_RELEASE_URL)/php-$(PHP_VERSION)-windows-amd64.exe" -o $@

.PHONY: $(ARCHIVES)/php_windows_amd64
$(ARCHIVES)/php_windows_amd64: $(ARCHIVES)/php_windows.exe $(ARCHIVES)/cacert.pem

.PHONY: $(ARCHIVES)/cacert.pem
$(ARCHIVES)/cacert.pem:
	mkdir -p $(ARCHIVES)
	curl -fSL https://curl.se/ca/cacert.pem -o $@

.PHONY: FORCE
FORCE:

##@ Release

# Create and publish a release.
.PHONY: release
release: goreleaser clean $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php $(ARCHIVES)/cacert.pem
ifndef RSA_SIGNING_KEY_FILE
	$(error RSA_SIGNING_KEY_FILE is not set. Set it to the path of your RSA private key for APK signing.)
endif
ifndef GPG_SIGNING_KEY_FILE
	$(error GPG_SIGNING_KEY_FILE is not set. Set it to the path of your GPG private key for RPM signing.)
endif
	PHP_VERSION=$(PHP_VERSION) goreleaser release --clean

.PHONY: vendor-release
vendor-release: check-vendor .goreleaser.vendor.yaml goreleaser clean $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) $(ARCHIVES)/commands-vendor.json php ## Release a vendor CLI (needs VENDOR_NAME, VENDOR_BINARY)
	PHP_VERSION=$(PHP_VERSION) VENDOR_BINARY="$(VENDOR_BINARY)" VENDOR_NAME="$(VENDOR_NAME)" goreleaser release --clean --config=.goreleaser.vendor.yaml

# Check the goreleaser configs.
.PHONY: goreleaser-check
goreleaser-check: goreleaser
	PHP_VERSION=$(PHP_VERSION) goreleaser check --config=.goreleaser.yaml

.PHONY: goreleaser
goreleaser:
	command -v goreleaser >/dev/null || go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

.PHONY: repogen
repogen:
	command -v repogen >/dev/null || go install github.com/ralt/repogen/cmd/repogen@$(REPOGEN_VERSION)

.goreleaser.vendor.yaml: check-vendor
	cat .goreleaser.vendor.yaml.tpl | envsubst > .goreleaser.vendor.yaml

.PHONY: check-vendor
check-vendor:
ifndef VENDOR_NAME
	$(error VENDOR_NAME is undefined)
endif
ifndef VENDOR_BINARY
	$(error VENDOR_BINARY is undefined)
endif

##@ Test and lint

.PHONY: test
test: test-go test-php ## Run Go and PHP unit tests

.PHONY: test-go
test-go: ## Run Go unit tests (excludes integration tests)
	go test -v -race -cover -count=1 $$(go list ./... | grep -v /integration-tests)

.PHONY: test-php
test-php: composer-dev ## Run PHP unit tests (excludes slow tests)
	cd legacy && ./scripts/test/unit.sh

.PHONY: integration-test
integration-test: single ## Build the CLI and run integration tests
	cd integration-tests && go test -v -count=1 ./...

.PHONY: lint
lint: lint-gomod lint-golangci lint-php-cs-fixer lint-phpstan ## Run all linters

.PHONY: lint-gomod
lint-gomod: ## Check that go.mod is tidy
	go mod tidy -diff

.PHONY: lint-golangci
lint-golangci: ## Run golangci-lint
	golangci-lint run --timeout=2m

.PHONY: lint-php-cs-fixer
lint-php-cs-fixer: composer-dev ## Check PHP code style
	cd legacy && ./vendor/bin/php-cs-fixer check --config .php-cs-fixer.dist.php --diff

.PHONY: lint-phpstan
lint-phpstan: composer-dev ## Run PHPStan
	cd legacy && ./vendor/bin/phpstan analyse

# The embedded lint registry is transformed from https://meta.upsun.com/images by gen.go.
# The schemas in internal/lint/schema are maintained in this repository.
.PHONY: lint-assets
lint-assets: ## Refresh the embedded lint registry from upstream
	cd internal/lint/registry && go run gen.go

# Fail if the embedded lint registry is stale.
.PHONY: lint-assets-check
lint-assets-check: lint-assets
	git diff --exit-code -- internal/lint/registry/registry.json

# Installs dev dependencies, which a phar build removes.
.PHONY: composer-dev
composer-dev:
	cd legacy && composer install --no-interaction

##@ Clean

.PHONY: clean
clean: ## Remove the phar, command indexes, legacy dependencies and caches
	rm -f $(ARCHIVES)/platform.phar $(ARCHIVES)/commands*.json legacy/config/cache/container.php
	rm -rf legacy/vendor legacy/vendor-bin/box/vendor

.PHONY: clean-all
clean-all: clean npm-clean ## Also remove PHP binaries and build output
	rm -rf $(ARCHIVES) dist .goreleaser.vendor.yaml

.PHONY: npm-clean
npm-clean: ## Remove npm/dist working directory
	rm -rf npm/dist

# ----- npm distribution -----
# See npm/README.md.

# Build npm tarballs from existing GoReleaser archives in dist/.
.PHONY: npm-pack
npm-pack:
	bash npm/scripts/build.sh

# Publish npm tarballs (requires npm auth). NPM_TAG=latest|next, DRY_RUN=1 to dry-run.
.PHONY: npm-publish
npm-publish:
	bash npm/scripts/publish.sh
