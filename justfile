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

standardgo := "github.com/amberpixels/standardgo/cmd/standardgo@latest"

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
# Go linting is standardgo (https://github.com/amberpixels/standardgo): the
# ruleset and the golangci-lint engine ship compiled into one binary, so there
# is no .golangci.yml here and there must not be one - it would not be read.
# It runs via `go run`, not a go.mod tool directive, so the linter's own
# dependencies stay out of ele's go.mod.
# @latest rather than a pin: standardgo is in no go.mod, so dependabot cannot
# see it and nothing would ever bump it. The trade is reproducibility - a
# standardgo release can turn lint red on a change that touched nothing.
#
# Convention: `fmt` rewrites to canonical form and decides nothing, `fix`
# applies judgement (run it on a clean tree and read the diff), `lint` only
# reports and never writes.

# Format Go code - rewrites to canonical form
[group('fmt')]
[no-exit-message]
fmt:
    go run {{ standardgo }} fmt ./...
    go mod tidy

# Auto-fix lint findings - mutates and makes judgement calls
[group('fix')]
[no-exit-message]
fix:
    go run {{ standardgo }} ./... --fix

# Lint Go code with standardgo - reports findings, changes nothing
[group('lint')]
[no-exit-message]
lint: vet
    go run {{ standardgo }} ./...

# standardgo runs govet too; `vet` stays its own recipe so it can run without
# building the lint engine.

# Vet - the real `go vet` across all packages - immutable
[group('lint')]
vet:
    go vet ./...

# --- Testing ---

# Run all tests, e.g. `just test -run Frame -v`
[group('test')]
test *ARGS:
    go test {{ARGS}} ./...

# --- Codegen ---
#
# The parser's message table comes from PostgreSQL's own pg_dump catalogs, so a
# new major is a data change: run `gen-messages` when one ships, read the drift
# summary it prints, and commit what moved.

# Regenerate the message table from upstream release tags - fetches over the network
[group('gen')]
gen-messages:
    go run ./tools/pgmsg

# Regenerate from the committed msgid snapshots; fails if anything moved - immutable
[group('gen')]
gen-messages--check:
    go run ./tools/pgmsg -offline
    git diff --exit-code internal/parser/messages_gen.go tools/pgmsg/testdata

# --- CI & release ---

# Everything CI runs, in CI's order (build, vet, lint, test) - immutable
[group('ci')]
ci: build lint test

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
