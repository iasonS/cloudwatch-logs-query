// cwq runs one CloudWatch Logs Insights query in a single command: it starts the query,
// waits for it to end and prints the rows.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/spf13/pflag"

	"github.com/iasonS/cloudwatch-logs-query/internal/output"
	"github.com/iasonS/cloudwatch-logs-query/internal/query"
	"github.com/iasonS/cloudwatch-logs-query/internal/window"
)

const usageHeader = `cwq runs one CloudWatch Logs Insights query and prints the result.

Usage:
  cwq -g GROUP [flags] 'QUERY'
  cwq -g GROUP [flags] -f FILE
  echo 'QUERY' | cwq -g GROUP [flags]

Flags:
`

// flags holds the command line as typed.
type flags struct {
	groups   []string
	prefixes []string
	since    string
	from     string
	to       string
	file     string
	json     bool
	csv      bool
	ptr      bool
	local    bool
	quiet    bool
	profile  string
	region   string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cwq:", err)
		os.Exit(1)
	}
}

func run() error {
	f, args := parseFlags()
	if err := f.check(); err != nil {
		return err
	}
	text, err := readQuery(args, f.file)
	if err != nil {
		return err
	}
	span, err := window.Flags{Since: f.since, From: f.from, To: f.to}.Resolve(time.Now())
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	started := time.Now()
	result, err := runQuery(ctx, f, query.Request{Query: text, From: span.From, To: span.To})
	if err != nil {
		return err
	}
	return f.print(result, time.Since(started))
}

func parseFlags() (flags, []string) {
	var f flags
	pflag.StringArrayVarP(&f.groups, "group", "g", nil, "log group `name` to query; repeat for more")
	pflag.StringArrayVar(&f.prefixes, "group-prefix", nil, "also query every log group whose name starts with `prefix`; repeatable")
	pflag.StringVar(&f.since, "since", "", "how far back from now to look, a `duration` such as 30m, 3h or 2d (default 1h)")
	pflag.StringVar(&f.from, "from", "", "start `time`, RFC 3339 with a zone, such as 2026-01-05T10:00Z")
	pflag.StringVar(&f.to, "to", "", "end `time`, RFC 3339 with a zone (default now)")
	pflag.StringVarP(&f.file, "file", "f", "", "read the query from `file`")
	pflag.BoolVar(&f.json, "json", false, "print one JSON object per row")
	pflag.BoolVar(&f.csv, "csv", false, "print CSV")
	pflag.BoolVar(&f.ptr, "ptr", false, "show the @ptr field")
	pflag.BoolVar(&f.local, "local", false, "print timestamps in the local time zone instead of UTC")
	pflag.BoolVar(&f.quiet, "quiet", false, "do not print the statistics line on stderr")
	pflag.StringVar(&f.profile, "profile", "", "AWS `profile` (default: the AWS SDK's own choice, such as AWS_PROFILE)")
	pflag.StringVar(&f.region, "region", "", "AWS `region` (default: the AWS SDK's own choice, such as AWS_REGION)")
	pflag.CommandLine.SortFlags = false
	pflag.Usage = func() {
		fmt.Fprint(os.Stderr, usageHeader)
		pflag.PrintDefaults()
	}
	pflag.Parse()
	return f, pflag.Args()
}

// check refuses flags that cannot work together, before anything talks to AWS.
func (f flags) check() error {
	if len(f.groups) == 0 && len(f.prefixes) == 0 {
		return errors.New("name at least one log group with -g or --group-prefix (see cwq --help)")
	}
	if f.json && f.csv {
		return errors.New("use --json or --csv, not both")
	}
	return nil
}

// readQuery takes the query from the argument, the file or stdin.
func readQuery(args []string, file string) (string, error) {
	text, err := querySource(args, file)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("the query is empty")
	}
	return text, nil
}

func querySource(args []string, file string) (string, error) {
	if len(args) > 1 {
		return "", errors.New("the query must be one argument; put it in quotes")
	}
	if len(args) == 1 && file != "" {
		return "", errors.New("give the query as an argument or with -f, not both")
	}
	if len(args) == 1 {
		return args[0], nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		return string(data), err
	}
	return readStdin()
}

func readStdin() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	// A terminal on stdin would wait for typing with no prompt.
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("no query: give it as an argument, with -f or on stdin")
	}
	data, err := io.ReadAll(os.Stdin)
	return string(data), err
}

func runQuery(ctx context.Context, f flags, request query.Request) (query.Result, error) {
	client, err := newClient(ctx, f.profile, f.region)
	if err != nil {
		return query.Result{}, err
	}
	runner := &query.Runner{Logs: client, Poll: time.Second}

	request.Groups, err = runner.Groups(ctx, f.groups, f.prefixes)
	if err != nil {
		return query.Result{}, err
	}
	return runner.Run(ctx, request)
}

// newClient takes credentials and region from the AWS SDK's default chain, with --profile
// and --region on top.
func newClient(ctx context.Context, profile, region string) (*cloudwatchlogs.Client, error) {
	var options []func(*config.LoadOptions) error
	if profile != "" {
		options = append(options, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		options = append(options, config.WithRegion(region))
	}

	awsConfig, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	if awsConfig.Region == "" {
		return nil, errors.New("no AWS region: use --region, AWS_REGION or a profile with a region")
	}
	return cloudwatchlogs.NewFromConfig(awsConfig), nil
}

func (f flags) print(result query.Result, elapsed time.Duration) error {
	err := output.Rows(os.Stdout, result.Rows, output.Options{Format: f.format(), ShowPtr: f.ptr, Local: f.zone()})
	if err != nil || f.quiet {
		return err
	}
	return output.Stats(os.Stderr, result.Stats, elapsed)
}

func (f flags) format() output.Format {
	if f.json {
		return output.JSON
	}
	if f.csv {
		return output.CSV
	}
	return output.Table
}

func (f flags) zone() *time.Location {
	if f.local {
		return time.Local
	}
	return nil
}
