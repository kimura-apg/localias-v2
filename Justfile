# This Justfile contains rules/targets/scripts/commands that are used when
# developing. Unlike a Makefile, running `just <cmd>` will always invoke
# that command. For more information, see https://github.com/casey/just
#
#
# this setting will allow passing arguments through to tasks, see the docs here
# https://just.systems/man/en/chapter_24.html#positional-arguments
set positional-arguments

# Go 1.24+ toolchains default GOEXPERIMENT to include "jsonv2". Under that
# experiment, `encoding/json.RawMessage` is backed by a different
# (`jsontext.Value`) runtime type than the one caddy/v2 v2.10.0's
# reflection-based module loader (`caddy.Context.LoadModule`) was written
# against, which makes it silently return a nil module value for any
# inline-keyed `json.RawMessage` field -- including the "storage" global
# option this project always sets. Symptom: `caddy.Load` panics with
# "interface conversion: interface is nil, not caddy.StorageConverter" the
# moment the daemon actually tries to start, on every alias, wildcard or
# not. Building (and testing, since some tests start a real Caddy
# instance) with the experiment off avoids the bug entirely; it only
# affects compile-time stdlib selection, so binaries built this way don't
# need the env var at runtime.
export GOEXPERIMENT := "nojsonv2"

# print all available commands by default
default:
  just --list

# remove any previously-built binaries
clean:
  rm -rf ./bin
  rm -rf ./result

# run the test suite
test *args='./...':
  go test "$@"

# lint all
lint:
  just lint-go
  just lint-nix
# lint/fix go code with golangci-lint
lint-go *args='./...':
  go mod tidy
  golangci-lint config verify --config .golangci.yaml
  golangci-lint run --fix --config .golangci.yaml $@
# lint/fix nix code with nixpkgs-fmt
lint-nix:
  git ls-files '*.nix' | nixpkgs-fmt

# build the localias cli
build:
  #!/usr/bin/env bash
  ldflags=$(./scripts/golang-ldflags.sh)
  go build -ldflags "$ldflags" -o bin/localias ./cmd/localias-v2

