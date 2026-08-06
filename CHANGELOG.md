# Changelog

Release notes are generated from commits by goreleaser; this file documents the breaking changes of major releases.

## v2.0.0

### Breaking changes

- **Job output decoration is on by default.** Every line of every job and boot job is prefixed with `[<RFC3339 timestamp>] [<job name>] `. Opt out globally with `mittnite up --job-log-timestamps=false --job-log-name-prefix=false` or `MITTNITE_JOB_LOG_TIMESTAMPS=0` / `MITTNITE_JOB_LOG_NAME_PREFIX=0`; opt out per job with `enableTimestamps = false` / `enableNamePrefix = false` (explicit per-job values always win, as before).
- **Decorated output is forwarded line-wise through mittnite.** Single lines longer than 64 KiB are forwarded in chunks, and other jobs' output may interleave between the chunks of such a line on a shared target. Jobs that write binary data or machine-parsed output (e.g. JSON log lines consumed by a strict collector) to stdout/stderr should opt out per job — with both options disabled, the output streams are attached directly and stay byte-identical to v1.
- **`MITTNITE_JOB_LOG_*` semantics changed:** unset now means *enabled*; unparsable values fall back to *enabled*, with a startup warning naming the assumed value.
- **Watch `preCommand`/`postCommand` output is now decorated** with the owning job's timestamp/name prefix.
- The Docker tags `stable` and `latest` on quay.io move to v2 with this release — pin `quay.io/mittwald/mittnite:v1` to defer the migration.

### Other changes

- `Layout` and `RFC850` are now accepted `timestampFormat` values; both were documented but previously warned "unknown timestamp format" and fell back to RFC3339.
- The "logging with timestamp layout" message moved from info to debug level — it fired once per job start *and restart*.
- Successful `canFail` boot jobs no longer log a spurious "job failed, but is allowed to fail" warning with an empty error.
- Persistent write errors on a broken log target are logged once per failure streak instead of once per output line.
- Running `mittnite` without a subcommand falls back to `up` again — it crashed on a nil function since v1.x (broken in `cdb2ecf`, 2023).

## v1 and earlier

See the [GitHub releases](https://github.com/mittwald/mittnite/releases).
