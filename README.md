# cwq

Runs one CloudWatch Logs Insights query in a single command: it starts the query, waits for
it to finish and prints the rows. No more `aws logs start-query`, polling
`aws logs get-query-results` and formatting the JSON by hand.

## Examples

    cwq -g /aws/eks/my-cluster/application --since 3h 'fields @timestamp, @message | filter @message like /ERROR/ | limit 20'
    cwq -g group-a -g group-b --from 2026-01-05T10:00Z --to 2026-01-05T11:00Z -f query.txt
    echo 'stats count() by bin(5m)' | cwq -g my-group --since 1d
    cwq --group-prefix /aws/lambda/ --json 'filter @message like /timeout/'

- `-g` can be repeated. `--group-prefix` adds every log group whose name starts with the prefix.
- `--since` takes 30m, 3h, 2d and so on (default 1h). `--from` and `--to` take RFC 3339 times
  with a zone; a time without one is refused. Without `--to` the window ends now.
- The query comes from the argument, from `-f file`, or from stdin.
- Credentials and region come from the usual AWS SDK chain; `--profile` and `--region` override.
- Ctrl-C while waiting stops the query (StopQuery), so it does not keep running.
- The exit code is 1 when the query ends as Failed, Cancelled or Timeout.

AWS permissions: `logs:StartQuery`, `logs:GetQueryResults`, `logs:StopQuery`, and
`logs:DescribeLogGroups` for `--group-prefix`.

## Output

- Default: an aligned table, one line per row; newlines inside a value show as `\n`.
- `--json`: one JSON object per row. `--csv`: a header line, then the rows.
- `@ptr` is hidden unless you pass `--ptr`.
- Timestamps are printed as Logs Insights returns them, in UTC, and the table header says so.
  `--local` converts them to your time zone and adds the zone to each value.
- After the rows, one line on stderr: records matched, records scanned, bytes scanned and
  the time taken. `--quiet` hides it.

## Cost

Logs Insights bills per GB of log data scanned, not per row returned. The stderr line shows
the bytes each query scanned. A shorter time window and fewer log groups scan less.

## Build

Go runs in Docker, nothing to install:

    make test     # go vet and go test
    make build    # ./cwq for this machine

With Go installed, `go build ./cmd/cwq` works too.
