// Package query runs one CloudWatch Logs Insights query: it starts the query, waits for it
// to end and collects every row.
package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// stopTimeout bounds the StopQuery call made after Ctrl-C.
const stopTimeout = 10 * time.Second

var errInterrupted = errors.New("interrupted")

// Logs is the part of the CloudWatch Logs API that cwq calls. *cloudwatchlogs.Client has
// these methods; tests use a fake.
type Logs interface {
	DescribeLogGroups(ctx context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error)
	StartQuery(ctx context.Context, in *cloudwatchlogs.StartQueryInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error)
	GetQueryResults(ctx context.Context, in *cloudwatchlogs.GetQueryResultsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error)
	StopQuery(ctx context.Context, in *cloudwatchlogs.StopQueryInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StopQueryOutput, error)
}

// Request is one query to run.
type Request struct {
	Groups []string
	Query  string
	From   time.Time
	To     time.Time
}

// Field is one named value in a result row.
type Field struct {
	Name  string
	Value string
}

// Stats are the numbers Logs Insights reports for a finished query.
type Stats struct {
	RecordsMatched float64
	RecordsScanned float64
	BytesScanned   float64
}

// Result is what a finished query returned. Rows need not all have the same fields.
type Result struct {
	Rows  [][]Field
	Stats Stats
}

// Runner runs queries against one CloudWatch Logs client.
type Runner struct {
	Logs Logs
	Poll time.Duration // wait between two status checks
}

// Run starts the query, waits until it ends and returns all its rows. A query that ends
// in any state but Complete is an error. When waiting stops early, after Ctrl-C or an
// error, the query is stopped so it does not keep running.
func (r *Runner) Run(ctx context.Context, request Request) (Result, error) {
	id, err := r.start(ctx, request)
	if err != nil {
		return Result{}, err
	}

	first, err := r.wait(ctx, id)
	if err != nil {
		return Result{}, r.stopAfter(ctx, id, err)
	}
	if first.Status != types.QueryStatusComplete {
		return Result{}, fmt.Errorf("query %s ended as %s", id, first.Status)
	}

	rows, err := r.allRows(ctx, id, first)
	if err != nil {
		return Result{}, err
	}
	return Result{Rows: rows, Stats: statsOf(first.Statistics)}, nil
}

func (r *Runner) start(ctx context.Context, request Request) (string, error) {
	out, err := r.Logs.StartQuery(ctx, &cloudwatchlogs.StartQueryInput{
		LogGroupNames: request.Groups,
		QueryString:   aws.String(request.Query),
		StartTime:     aws.Int64(request.From.Unix()),
		EndTime:       aws.Int64(request.To.Unix()),
	})
	if err != nil {
		return "", err
	}
	if aws.ToString(out.QueryId) == "" {
		return "", errors.New("StartQuery returned no query id")
	}
	return aws.ToString(out.QueryId), nil
}

// wait checks the query's status until it is no longer scheduled or running, and returns
// the last answer, which holds the first page of rows.
func (r *Runner) wait(ctx context.Context, id string) (*cloudwatchlogs.GetQueryResultsOutput, error) {
	for {
		out, err := r.Logs.GetQueryResults(ctx, &cloudwatchlogs.GetQueryResultsInput{QueryId: aws.String(id)})
		if ctx.Err() != nil {
			return nil, errInterrupted
		}
		if err != nil {
			return nil, err
		}
		if !stillRunning(out.Status) {
			return out, nil
		}

		select {
		case <-ctx.Done():
			return nil, errInterrupted
		case <-time.After(r.Poll):
		}
	}
}

func stillRunning(status types.QueryStatus) bool {
	return status == types.QueryStatusScheduled || status == types.QueryStatusRunning
}

// stopAfter stops a query that cwq stopped waiting for, and says whether that worked.
func (r *Runner) stopAfter(ctx context.Context, id string, cause error) error {
	if err := r.stop(ctx, id); err != nil {
		return fmt.Errorf("%w; could not stop query %s, it may still be running: %v", cause, id, err)
	}
	return fmt.Errorf("%w; query %s stopped", cause, id)
}

// stop gets a context of its own because ctx may be the one Ctrl-C just cancelled.
func (r *Runner) stop(ctx context.Context, id string) error {
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()

	out, err := r.Logs.StopQuery(stopCtx, &cloudwatchlogs.StopQueryInput{QueryId: aws.String(id)})
	if err != nil {
		return err
	}
	if !out.Success {
		return errors.New("StopQuery did not report success")
	}
	return nil
}

// allRows returns the rows of the first page and of every page after it; a large result
// comes in several pages.
func (r *Runner) allRows(ctx context.Context, id string, first *cloudwatchlogs.GetQueryResultsOutput) ([][]Field, error) {
	rows := toRows(first.Results)
	token := aws.ToString(first.NextToken)
	for token != "" {
		page, err := r.Logs.GetQueryResults(ctx, &cloudwatchlogs.GetQueryResultsInput{
			QueryId:   aws.String(id),
			NextToken: aws.String(token),
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, toRows(page.Results)...)

		next := aws.ToString(page.NextToken)
		if next == token {
			return nil, errors.New("GetQueryResults returned the same page token twice")
		}
		token = next
	}
	return rows, nil
}

func toRows(results [][]types.ResultField) [][]Field {
	rows := make([][]Field, 0, len(results))
	for _, result := range results {
		row := make([]Field, 0, len(result))
		for _, field := range result {
			row = append(row, Field{Name: aws.ToString(field.Field), Value: aws.ToString(field.Value)})
		}
		rows = append(rows, row)
	}
	return rows
}

func statsOf(statistics *types.QueryStatistics) Stats {
	if statistics == nil {
		return Stats{}
	}
	return Stats{
		RecordsMatched: statistics.RecordsMatched,
		RecordsScanned: statistics.RecordsScanned,
		BytesScanned:   statistics.BytesScanned,
	}
}
