export GOWORK := off
export GOTOOLCHAIN := go1.27.0
MANIFEST ?= wippy.build.json
OUTPUT ?= dist/application
WIPPY ?= wippy
.PHONY: check test build tools example-pack smoke
check: test
	go vet ./...
	@test -z "$$(gofmt -l cmd internal)"
test:
	go test -race ./...
tools:
	go build -trimpath -o dist/wippy-builder ./cmd/wippy-builder
build: tools
	dist/wippy-builder build "$(MANIFEST)" --output "$(OUTPUT)"
example-pack: tools
	cd examples/hello && $(WIPPY) lint
	dist/wippy-builder pack examples/hello/wippy.build.json --toolchain "$(WIPPY)"
smoke:
	WIPPY_TEST_BINARY="$(abspath $(OUTPUT))" go test ./internal/assemble -run 'Test(Standalone|Arguments)' -count=1 -v
