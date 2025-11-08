.PHONY: all
.PHONY: test
.PHONY: vet
.PHONY: staticcheck
.PHONY: lint
.PHONY: install-deps

all: vet lint staticcheck test

test:
	@echo "*** $@"
	@go test -timeout 2m ./...

vet:
	@echo "*** $@"
	@go vet ./...

staticcheck:
	@echo "*** $@"
	@staticcheck ./...

lint:
	@echo "*** $@"
	@revive ./...

install-deps:
	@go install github.com/mgechev/revive@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
