# cactusInv — `make check` is the quality bar (CLAUDE.md §3).
SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
GO_PKGS := ./...
COVER_MIN := 80

.PHONY: help gen fmt fmt-check lint vet test cover test-int remote-test-int vuln contracts web-install web-check build image check \
        install uninstall status backup

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

gen: web-install ## regenerate sqlc, oapi-codegen and web API types
	sqlc generate
	oapi-codegen -config api/openapi/oapi-codegen.yaml api/openapi/cactusinv.yaml
	pnpm -C web gen:api

fmt: ## format Go code
	gofumpt -w cmd internal web migrations

fmt-check:
	@out="$$(gofumpt -l cmd internal web migrations | grep -v '\.gen\.go$$' | grep -v sqlcgen || true)"; \
	  if [[ -n "$$out" ]]; then echo "gofumpt needed:"; echo "$$out"; exit 1; fi

lint: ## golangci-lint (incl. depguard layering rules)
	golangci-lint run $(GO_PKGS)

vet:
	go vet $(GO_PKGS)

test: ## unit tests
	go test -race -count=1 $(GO_PKGS)

cover: ## unit tests with coverage gates on domain and app
	go test -race -count=1 -coverprofile=coverage.out $(GO_PKGS) >/dev/null
	@for p in internal/domain internal/app; do \
	  pct=$$(go test -count=1 -cover ./$$p | sed -E 's/.*coverage: ([0-9.]+)%.*/\1/'); \
	  echo "$$p coverage $$pct%"; \
	  awk -v p="$$pct" -v m=$(COVER_MIN) 'BEGIN{exit !(p>=m)}' || { echo "$$p below $(COVER_MIN)%"; exit 1; }; \
	done

test-int: ## integration tests against MariaDB 11 (Docker); migrations up/down/up per test
	go test -race -count=1 -tags integration ./internal/adapters/mariadb/

# remote-test-int runs test-int on a Docker host over ssh, for machines without Docker.
# It ships the working tree (tracked files plus untracked, not-ignored ones, uncommitted
# changes included) as a tarball, unpacks it into a fresh $(REMOTE_DIR), and runs the tests
# in the pinned Go image with the host's Docker socket. testcontainers reaches the MariaDB
# container through the host's published ports (host.docker.internal → host gateway).
# label=disable: on an SELinux host the socket is otherwise refused to the container.
REMOTE      ?=
REMOTE_DIR  ?= /data/cactusinv/src
REMOTE_GOCACHE ?= /data/cactusinv/gocache
GO_IMAGE    ?= golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c

remote-test-int: ## integration tests on REMOTE=user@host (required) via ssh + Docker (working tree incl. uncommitted changes)
	@[ -n "$(REMOTE)" ] || { echo "set REMOTE=user@host (a Docker host for the integration tests)"; exit 1; }
	@case "$(REMOTE_DIR)/" in /data/cactusinv/?*/) ;; *) echo "REMOTE_DIR must be below /data/cactusinv/"; exit 1;; esac
	@case "$(REMOTE_GOCACHE)/" in /data/cactusinv/?*/) ;; *) echo "REMOTE_GOCACHE must be below /data/cactusinv/"; exit 1;; esac
	git ls-files -z -co --exclude-standard \
	  | while IFS= read -r -d '' f; do [[ -f "$$f" ]] && printf '%s\0' "$$f"; done \
	  | COPYFILE_DISABLE=1 tar --null -T - -czf - \
	  | ssh $(REMOTE) 'set -euo pipefail; rm -rf "$(REMOTE_DIR)"; mkdir -p "$(REMOTE_DIR)" "$(REMOTE_GOCACHE)/mod" "$(REMOTE_GOCACHE)/build"; \
	      tar --warning=no-unknown-keyword -xzf - -C "$(REMOTE_DIR)"; \
	      docker run --rm --security-opt label=disable \
	        -v /var/run/docker.sock:/var/run/docker.sock \
	        --add-host=host.docker.internal:host-gateway -e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal \
	        -e GOTOOLCHAIN=local \
	        -v "$(REMOTE_DIR)":/src -v "$(REMOTE_GOCACHE)/mod":/go/pkg/mod -v "$(REMOTE_GOCACHE)/build":/root/.cache/go-build \
	        -w /src $(GO_IMAGE) go test -race -count=1 -tags integration ./internal/adapters/mariadb/'

vuln: ## known-vulnerability scan
	govulncheck $(GO_PKGS)

contracts: web-install ## generated code must match the contract and queries
	sqlc diff
	tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
	  sed "s#^output:.*#output: $$tmp/api.gen.go#" api/openapi/oapi-codegen.yaml >"$$tmp/cfg.yaml" && \
	  oapi-codegen -config "$$tmp/cfg.yaml" api/openapi/cactusinv.yaml && \
	  diff -q "$$tmp/api.gen.go" internal/adapters/http/api.gen.go && \
	  (cd web && pnpm -s exec openapi-typescript ../api/openapi/cactusinv.yaml -o "$$tmp/schema.d.ts" >/dev/null) && \
	  diff -q "$$tmp/schema.d.ts" web/src/lib/api/schema.d.ts && echo "contracts in sync"

web-install:
	@[[ -d web/node_modules ]] || pnpm -C web install --frozen-lockfile

web-check: web-install ## web lint, typecheck, tests, build
	pnpm -C web check

build: web-check ## build the binary with the web app embedded
	CGO_ENABLED=0 go build -trimpath -o bin/cactusinv ./cmd/cactusinv

image: ## build the container image
	docker build -t cactusinv:latest .

check: fmt-check lint vet cover test-int vuln contracts web-check ## THE quality bar

install: ## deploy on Docker Desktop
	./deploy.sh --install --dockerdesktop
uninstall: ## remove from Docker Desktop, keeping data
	./deploy.sh --uninstall --dockerdesktop
status:
	./deploy.sh --status --dockerdesktop
backup: ## take a database backup now
	./deploy.sh --backup-now --dockerdesktop
