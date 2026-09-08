.PHONY: check test build
MANIFEST ?= wippy.build.json
OUTPUT ?= dist/application
check: test
	python3 -m py_compile builder.py

test:
	python3 -m unittest discover -s tests -v

build:
	python3 builder.py build "$(MANIFEST)" --output "$(OUTPUT)"

WIPPY ?= wippy
.PHONY: example-pack
example-pack:
	cd examples/hello && $(WIPPY) lint
	cd examples/hello && $(WIPPY) pack hello.wapp --meta namespace=example.hello --meta name=hello --meta version=1.0.0 --silent

.PHONY: smoke
smoke:
	python3 tests/standalone.py "$(OUTPUT)"
