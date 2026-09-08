export GOWORK := off
export GOTOOLCHAIN := go1.27.0
MANIFEST ?= wippy.build.json
OUTPUT ?= dist/application
WIPPY ?= wippy
BUILDER_REVISION ?=
.PHONY: check test build tools example-pack smoke
check: repository-check test
	go vet ./...
	@test -z "$$(gofmt -l cmd internal)"
ACTIONLINT ?= go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
GITLEAKS ?= go run github.com/zricethezav/gitleaks/v8@v8.30.1
.PHONY: repository-check
repository-check:
	@command -v shellcheck >/dev/null || { echo 'Install ShellCheck to validate workflow scripts.' >&2; exit 1; }
	$(ACTIONLINT)
	$(GITLEAKS) git --log-opts=--all --redact --no-banner
	$(GITLEAKS) dir --redact --no-banner
test:
	go test -race ./...
tools:
	go build -trimpath -buildvcs=$(if $(BUILDER_REVISION),false,auto) -ldflags "-X github.com/wippyai/builder/internal/assemble.buildRevision=$(BUILDER_REVISION)" -o dist/wippy-builder ./cmd/wippy-builder
build: tools
	dist/wippy-builder build "$(MANIFEST)" --output "$(OUTPUT)"
example-pack: tools
	cd examples/hello && $(WIPPY) lint
	dist/wippy-builder pack examples/hello/wippy.build.json --toolchain "$(WIPPY)"
smoke:
	WIPPY_TEST_BINARY="$(abspath $(OUTPUT))" go test ./internal/assemble -run 'Test(Standalone|Arguments)' -count=1 -v

GORELEASER ?= goreleaser
export RELEASE_VERSION ?= 0.0.0-dev
.PHONY: release
release: check
	$(GORELEASER) check
	$(GORELEASER) release --snapshot --clean --skip=publish
