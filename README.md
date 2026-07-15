<p align="center">
  <img src="logo.svg" alt="ele" width="200">
</p>

<div align="center">

### Quiet the elephant.

A drop-in `pg_restore` wrapper that swaps its noisy stderr for live progress and a classified error summary.

[![Go Version](https://img.shields.io/github/go-mod/go-version/amberpixels/ele)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)

</div>

---

`pg_restore --verbose` does the job, but it floods the terminal with thousands
of lines of stderr. Most of it is benign noise - `does not exist` during a
`--clean` run, missing roles from an RDS dump - and the one error that actually
matters scrolls past unseen. So people pipe it to `| tail`, or `|| true`, and
hope.

`ele` wraps the same command. It swallows the firehose and repaints a compact,
per-phase progress view in its place - pre-data, data, post-data - with an
error panel that groups and classifies what went wrong. The raw output is
always kept in a log file, and the exit code reflects whether any *real* error
happened, not just whether pg_restore was noisy.

> [!NOTE]
> Early build, offline so far. `ele <dump>` prints the parsed restore plan, and
> `ele --replay <dump> <log>` runs a captured `pg_restore` stderr log through
> the parser and aggregator and prints a summary. Wrapping a live `pg_restore`
> run with an in-place progress view is next.

## Install

```sh
go install github.com/amberpixels/ele/cmd/ele@latest
```

## Quick Start

Point `ele` at a dump to see the plan it parses from the archive:

```console
$ ele mydb.dump
format:  custom
entries: 20

  pre-data   8
  data       4
  post-data  8
```

For a directory-format dump (`pg_dump -Fd`), the data phase is sized in bytes,
since the per-table files can be stat'd up front:

```console
$ ele mydb.dir
format:  directory
entries: 20

  pre-data   8
  data       4  (29.4 KB)
  post-data  8
```

## How It Works

Preflight shells out to `pg_restore -l <dump>` (with `LC_ALL=C` so the listing
is deterministic) and parses the table of contents into a restore plan: every
object, keyed by its dump id, sorted into the pre-data / data / post-data phase
`pg_restore` will replay it in. The section isn't printed in the listing, so
`ele` reconstructs it from each object's type the same way `pg_dump` assigns it.

Preflight touches no database - it only reads the dump file. The phase counts it
produces are exact denominators, not estimates, which is what makes honest
progress bars possible once the wrapper lands.

## Requirements

- `pg_restore` on your `PATH` (PostgreSQL 13-18 client tools).

## Feedback

`ele` is a solo, opinionated project - but if you stumbled upon it and have
ideas, questions, or bug reports, an [issue](https://github.com/amberpixels/ele/issues) is always welcome :)

## License

[MIT](LICENSE) © [amberpixels](https://amberpixels.io)
