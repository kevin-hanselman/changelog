.PHONY: install fmt lint test% %-test-cov clean tidy loc mocks hyperfine

GOPATH ?= ~/go
GOBIN ?= $(GOPATH)/bin

changelog: test-all
	go build -o changelog -ldflags "-s -w"

install: $(GOBIN)/changelog

$(GOBIN)/changelog: changelog
	cp changelog $(GOBIN)

fmt: $(GOBIN)/goimports
	goimports -w .
	gofmt -s -w .

lint: $(GOBIN)/golint
	go vet ./...
	golint ./...

test: fmt lint
	go test -short ./...

test-all: fmt lint
	go test -cover -race ./...

bench: test
	go test ./... -benchmem -bench .

%-test-cov: %-test.coverage
	go tool cover -html=$<

unit-test.coverage:
	go test -short ./... -coverprofile=$@

int-test.coverage:
	go test -run Integration ./... -coverprofile=$@

all-test.coverage:
	go test ./... -coverprofile=$@

deep-lint:
	docker run \
		--rm \
		-v $(shell pwd):/app \
		-w /app \
		golangci/golangci-lint:latest \
		golangci-lint run

clean:
	rm -f *.coverage depgraph.png mockery $(GOBIN)/changelog
	go clean ./...

tidy:
	go mod tidy -v

loc:
	tokei --sort lines --exclude 'mocks/'
	tokei --sort lines --exclude 'mocks/' --exclude '*_test.go'

mockery:
	curl -L https://github.com/vektra/mockery/releases/download/v2.2.1/mockery_2.2.1_Linux_x86_64.tar.gz \
		| tar -zxvf - mockery

mocks: mockery
	./mockery --all --output mocks

# The awk command removes all graph edge definitions that don't include changelog
depgraph.png:
	godepgraph -nostdlib . \
		| awk '/^[^"]/ || /changelog/ {print;}' \
		| dot -Tpng -o $@

$(GOBIN)/goimports:
	go install golang.org/x/tools/cmd/goimports

$(GOBIN)/golint:
	go install golang.org/x/lint/golint
