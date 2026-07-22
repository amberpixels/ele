import? 'justfile.local'

binary_name := "ele"
build_dir   := "bin"
binary      := build_dir / binary_name
cmd_dir     := "./cmd/ele"
version     := `git describe --tags --always --dirty 2>/dev/null || echo dev`

# Offline inputs for the plan/replay recipes. Dumps, listings and captured logs
# are all gitignored (they carry real object names), so this is a local scratch
# file - override per run: `just replay ele-20260722-161815.log latest.toc`
plan_file := "demo.loc"

# Default: list available commands
[private]
default:
    @just --list

# --- Build & run ---

# Build the binary into bin/
[group('dev')]
build:
    @mkdir -p {{build_dir}}
    go build -ldflags "-X main.version={{version}}" -o {{binary}} {{cmd_dir}}

# Install the binary to GOPATH/bin
[group('dev')]
install:
    go install -ldflags "-X main.version={{version}}" {{cmd_dir}}

# Run the wrapper with pg_restore args, e.g. `just run -j 4 -d myapp latest.dump`
[group('dev')]
run *ARGS: build
    ./{{binary}} {{ARGS}}

# Print the parsed restore plan for a dump or a saved `pg_restore -l` listing
[group('dev')]
plan file=plan_file: build
    ./{{binary}} --plan {{file}}

# Replay a captured stderr log through the live view - the offline dry-run
[group('dev')]
replay log plan=plan_file: build
    ./{{binary}} --replay {{plan}} {{log}}

# Remove build artifacts
[group('dev')]
clean:
    rm -rf {{build_dir}} dist

# --- Formatting & linting ---
#
# Convention: `fmt` is MUTABLE (formats + auto-fixes everything that can be
# fixed), `lint` is IMMUTABLE (reports issues only, never writes).

# Format + auto-fix Go code - mutable
[group('fmt')]
[no-exit-message]
fmt: _golangci-install
    golangci-lint fmt
    golangci-lint run --fix
    go mod tidy

# Lint with golangci-lint (check only) - immutable
[group('lint')]
[no-exit-message]
lint: _golangci-install vet
    golangci-lint run

# golangci-lint runs govet too, but CI invokes plain `go vet` as its own step,
# so a vet regression is caught even without golangci-lint installed.

# Vet - the real `go vet` across all packages - immutable
[group('lint')]
vet:
    go vet ./...

# Install golangci-lint (v2) if missing
[private]
_golangci-install:
    #!/usr/bin/env bash
    if ! command -v golangci-lint &>/dev/null; then
        go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
    fi

# --- Testing ---

# Run all tests, e.g. `just test -run Frame -v`
[group('test')]
test *ARGS:
    go test {{ARGS}} ./...

# --- CI & release ---

# Everything CI runs, in CI's order (build, vet, test) - immutable
[group('ci')]
ci: build vet test

# Validate the GoReleaser config
[group('release')]
release--check:
    goreleaser check

# Build a local release snapshot into dist/ - no tagging, no publishing
[group('release')]
release--snapshot:
    goreleaser release --snapshot --clean

# --- Aliases ---

alias t := test
alias b := build
alias snapshot := release--snapshot
