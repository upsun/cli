# The PHP version, and the cli-php-builds build number (rebuilt when a library is updated).
# Checksums of the binaries are pinned in PHP_SHA256SUMS. Updated by the update-php workflow.
PHP_VERSION = 8.5.12
PHP_BUILD = 2

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
PHP_RELEASE_TAG = php-$(PHP_VERSION)-$(PHP_BUILD)
PHP_RELEASE_URL = https://github.com/$(PHP_BUILDS_REPO)/releases/download/$(PHP_RELEASE_TAG)
PHP_SHA256SUMS = internal/legacy/php_sha256sums.txt

# Verifies a downloaded file against a checksum. Usage: $(call verify-sha256,file,checksum)
SHA256SUM := $(shell command -v sha256sum 2>/dev/null || echo shasum -a 256)
define verify-sha256
	@test -n "$(2)" || { echo "No checksum for $(1)" >&2; exit 1; }
	echo "$(2)  $(1)" | $(SHA256SUM) -c -
endef

# The pinned checksum of a PHP release asset. Usage: $(call php-sha256,asset-name)
php-sha256 = $(shell awk '$$2 == "$(1)" { print $$1 }' $(PHP_SHA256SUMS))

# Files embedded in the Go binary.
ARCHIVES = internal/legacy/archives
PHP_UNIX_BINARIES = $(foreach p,linux_amd64 linux_arm64 darwin_amd64 darwin_arm64,$(ARCHIVES)/php_$(p))
LEGACY_INDEXES = $(ARCHIVES)/commands-upsun.json $(ARCHIVES)/commands-platformsh.json

# Files that the legacy CLI phar is built from.
LEGACY_SOURCES := $(shell find legacy/bin legacy/src legacy/config legacy/resources -type f -not -path 'legacy/config/cache/*') \
	$(wildcard legacy/*.yaml legacy/*.tmpl.rc) legacy/constants.php legacy/box.json legacy/composer.lock \
	legacy/vendor-bin/box/composer.json legacy/vendor-bin/box/composer.lock

.PHONY: help
help: ## Show this help
	@if [ -t 1 ] && [ -z "$$NO_COLOR" ]; then bold=$$(printf '\033[1m'); reset=$$(printf '\033[0m'); fi; \
	awk -v bold="$$bold" -v reset="$$reset" 'BEGIN { FS = ":.*## " } \
		/^##@ / { printf "\n%s%s%s\n", bold, substr($$0, 5), reset } \
		/^[a-zA-Z0-9_-]+:.*## / { printf "%-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Targets with a "##" description are listed by `make help`. Those automated in CI workflows are not.

##@ Build

.PHONY: single
single: goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php ## Build a single binary for the current platform, in dist/
	PHP_VERSION=$(PHP_VERSION) goreleaser build --single-target --id=$(GORELEASER_ID) --snapshot --clean

.PHONY: snapshot
snapshot: goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php ## Build all release artifacts in dist/ without publishing (needs signing keys)
ifndef RSA_SIGNING_KEY_FILE
	$(error RSA_SIGNING_KEY_FILE is not set. Set it to the path of your RSA private key for APK signing, or use 'make snapshot-no-nfpm' to skip packaging.)
endif
ifndef GPG_SIGNING_KEY_FILE
	$(error GPG_SIGNING_KEY_FILE is not set. Set it to the path of your GPG private key for RPM signing, or use 'make snapshot-no-nfpm' to skip packaging.)
endif
	PHP_VERSION=$(PHP_VERSION) goreleaser release --snapshot --clean --skip=publish,announce

.PHONY: snapshot-no-nfpm
snapshot-no-nfpm: goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php ## Build release artifacts in dist/ without Linux packages or signing
	PHP_VERSION=$(PHP_VERSION) goreleaser release --snapshot --clean --skip=publish,announce,nfpm

.PHONY: vendor-snapshot
vendor-snapshot: check-vendor .goreleaser.vendor.yaml goreleaser $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) $(ARCHIVES)/commands-vendor.json php ## Build vendor CLI binaries for all platforms (needs VENDOR_NAME, VENDOR_BINARY)
	PHP_VERSION=$(PHP_VERSION) VENDOR_BINARY="$(VENDOR_BINARY)" VENDOR_NAME="$(VENDOR_NAME)" goreleaser build --snapshot --clean --config=.goreleaser.vendor.yaml

.PHONY: phar
phar: $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) ## Build the legacy CLI phar and its command indexes

.PHONY: php
php: $(ARCHIVES)/php_$(GOOS)_$(GOARCH) ## Download the PHP binary for the current platform

# Download PHP binaries for all platforms.
.PHONY: download-php
download-php: $(PHP_UNIX_BINARIES) $(ARCHIVES)/php_windows.exe

# Build the legacy CLI phar from the legacy/ subdirectory.
# The self:build command handles composer --no-dev rebuild, Box installation, and cache warming.
# The vendor directory is order-only, because self:build and composer-dev both rewrite it.
$(ARCHIVES)/platform.phar: $(LEGACY_SOURCES) | legacy/vendor/autoload.php
	mkdir -p $(ARCHIVES)
	cd legacy && php bin/platform self:build --no-interaction --output=../$@

# Index the legacy CLI's commands for each embedded config, so the Go layer can resolve abbreviations of its own
# commands and complete command and option names. HOME is isolated from user config. It runs outside any project, as
# some commands are hidden depending on the local project (e.g. environment:xdebug). The version is set, as by the Go
# wrapper, so that the CLI does not try to read it from Git. Usage: $(call legacy-index,phar,config-file)
define legacy-index
	tmp=$$(mktemp -d) && \
	prefix=$$(go run ./scripts/env-prefix $(2)) && \
	(cd $$tmp && env HOME=$$tmp CLI_CONFIG_FILE=$(abspath $(2)) "$${prefix}NO_LEGACY_WARNING=1" "$${prefix}APPLICATION_VERSION=0.0.0" \
		php $(abspath $(1)) list --all --format=json --no-interaction < /dev/null > $$tmp/commands.json) && \
	mv $$tmp/commands.json $@; \
	status=$$?; rm -rf $$tmp; exit $$status
endef

$(ARCHIVES)/commands-%.json: $(ARCHIVES)/platform.phar internal/config/%-cli.yaml
	$(call legacy-index,$<,internal/config/$*-cli.yaml)

$(ARCHIVES)/commands-vendor.json: $(ARCHIVES)/platform.phar internal/config/embedded-config.yaml
	$(call legacy-index,$<,internal/config/embedded-config.yaml)

legacy/vendor/autoload.php:
	cd legacy && composer install --no-interaction

# Records the PHP release tag, so that the PHP binaries are downloaded again when it changes.
$(ARCHIVES)/php-version: FORCE
	@mkdir -p $(ARCHIVES)
	@echo $(PHP_RELEASE_TAG) | cmp -s - $@ || echo $(PHP_RELEASE_TAG) > $@

$(PHP_UNIX_BINARIES): $(ARCHIVES)/php_%: $(ARCHIVES)/php-version
	curl -fSL "$(PHP_RELEASE_URL)/php-$(PHP_VERSION)-$(subst _,-,$*)" -o $@
	$(call verify-sha256,$@,$(call php-sha256,php-$(PHP_VERSION)-$(subst _,-,$*)))
	chmod +x $@

$(ARCHIVES)/php_windows.exe: $(ARCHIVES)/php-version
	curl -fSL "$(PHP_RELEASE_URL)/php-$(PHP_VERSION)-windows-amd64.exe" -o $@
	$(call verify-sha256,$@,$(call php-sha256,php-$(PHP_VERSION)-windows-amd64.exe))

.PHONY: $(ARCHIVES)/php_windows_amd64
$(ARCHIVES)/php_windows_amd64: $(ARCHIVES)/php_windows.exe

.PHONY: FORCE
FORCE:

##@ Release

# Create and publish a release.
.PHONY: release
release: goreleaser clean $(ARCHIVES)/platform.phar $(LEGACY_INDEXES) php
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

.PHONY: test-integration
test-integration: single ## Build the CLI and run integration tests
	cd integration-tests && go test -count=1 ./...

.PHONY: integration-test
integration-test: test-integration

.PHONY: lint
lint: lint-go lint-php ## Run all linters

.PHONY: lint-go
lint-go: ## Run Go linters (go mod tidy, golangci-lint)
	go mod tidy -diff
	golangci-lint run --timeout=2m

.PHONY: lint-php
lint-php: composer-dev ## Run PHP linters (php-cs-fixer, PHPStan)
	cd legacy && ./vendor/bin/php-cs-fixer check --config .php-cs-fixer.dist.php --diff
	cd legacy && ./vendor/bin/phpstan analyse

# The image registry, used by lint, is transformed from https://meta.upsun.com/images by gen.go.
.PHONY: update-registry
update-registry: ## Refresh the embedded image registry from meta.upsun.com
	cd pkg/registry && go run gen.go

# Fail if the embedded image registry is stale.
.PHONY: check-registry
check-registry: update-registry
	git diff --exit-code -- pkg/registry/registry.json

# The Flex config schema, used by lint, is from https://meta.upsun.com/schema/upsun (the upsun/meta repository).
# Fixed config is checked with the same schema and an overlay, internal/schema/fixed-overlay.json.
.PHONY: update-schema
update-schema: ## Refresh the embedded Flex config schema from meta.upsun.com
	cd internal/schema && go run gen.go

# Fail if the embedded Flex config schema is stale.
.PHONY: check-schema
check-schema: update-schema
	git diff --exit-code -- internal/schema/upsun-config-schema.json

# The API's OpenAPI spec, used to validate the mock API in tests, is from https://meta.upsun.com/openapi-spec.
.PHONY: update-openapi-spec
update-openapi-spec: ## Refresh the OpenAPI spec used by the mock API
	cd pkg/mockapi && go run gen.go

# Installs dev dependencies, which a phar build removes. PHPStan's binary marks that they are installed.
.PHONY: composer-dev
composer-dev: legacy/vendor/bin/phpstan

legacy/vendor/bin/phpstan: legacy/composer.lock
	cd legacy && composer install --no-interaction
	touch $@

##@ Clean

.PHONY: clean
clean: ## Remove the phar, command indexes, legacy dependencies and caches
	rm -f $(ARCHIVES)/platform.phar $(ARCHIVES)/commands*.json legacy/config/cache/container.php
	rm -rf legacy/vendor legacy/vendor-bin/box/vendor

.PHONY: clean-all
clean-all: clean ## Also remove PHP binaries and build output
	rm -rf $(ARCHIVES) dist npm/dist .goreleaser.vendor.yaml

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
