package query

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

const queryID = "query-1"

// fakeLogs answers like CloudWatch Logs would, from what a test puts in it.
type fakeLogs struct {
	groups   []string
	statuses []types.QueryStatus     // one per status check; the last one repeats
	pages    [][][]types.ResultField // rows of each page once the query is complete
	stats    *types.QueryStatistics
	pollErr  error
	cancel   context.CancelFunc // when set, called on the first status check, as Ctrl-C would

	started    *cloudwatchlogs.StartQueryInput
	polls      int
	stopped    []string
	stopCtxErr error
}

func (f *fakeLogs) DescribeLogGroups(_ context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	out := &cloudwatchlogs.DescribeLogGroupsOutput{}
	for _, name := range f.groups {
		if strings.HasPrefix(name, aws.ToString(in.LogGroupNamePrefix)) {
			out.LogGroups = append(out.LogGroups, types.LogGroup{LogGroupName: aws.String(name)})
		}
	}
	return out, nil
}

func (f *fakeLogs) StartQuery(_ context.Context, in *cloudwatchlogs.StartQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StartQueryOutput, error) {
	f.started = in
	return &cloudwatchlogs.StartQueryOutput{QueryId: aws.String(queryID)}, nil
}

func (f *fakeLogs) GetQueryResults(_ context.Context, in *cloudwatchlogs.GetQueryResultsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetQueryResultsOutput, error) {
	if f.cancel != nil {
		f.cancel()
	}
	if f.pollErr != nil {
		return nil, f.pollErr
	}
	if in.NextToken != nil {
		index, _ := strconv.Atoi(*in.NextToken)
		return f.page(index), nil
	}

	status := f.statuses[min(f.polls, len(f.statuses)-1)]
	f.polls++
	if status != types.QueryStatusComplete {
		return &cloudwatchlogs.GetQueryResultsOutput{Status: status}, nil
	}
	return f.page(0), nil
}

func (f *fakeLogs) page(index int) *cloudwatchlogs.GetQueryResultsOutput {
	out := &cloudwatchlogs.GetQueryResultsOutput{
		Status:     types.QueryStatusComplete,
		Results:    f.pages[index],
		Statistics: f.stats,
	}
	if index+1 < len(f.pages) {
		out.NextToken = aws.String(strconv.Itoa(index + 1))
	}
	return out
}

func (f *fakeLogs) StopQuery(ctx context.Context, in *cloudwatchlogs.StopQueryInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.StopQueryOutput, error) {
	f.stopped = append(f.stopped, aws.ToString(in.QueryId))
	f.stopCtxErr = ctx.Err()
	return &cloudwatchlogs.StopQueryOutput{Success: true}, nil
}

func row(nameValues ...string) []types.ResultField {
	var fields []types.ResultField
	for i := 0; i < len(nameValues); i += 2 {
		fields = append(fields, types.ResultField{Field: aws.String(nameValues[i]), Value: aws.String(nameValues[i+1])})
	}
	return fields
}

var request = Request{
	Groups: []string{"app"},
	Query:  "fields @message",
	From:   time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC),
	To:     time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC),
}

func TestRunWaitsAndReturnsEveryPage(t *testing.T) {
	logs := &fakeLogs{
		statuses: []types.QueryStatus{types.QueryStatusScheduled, types.QueryStatusRunning, types.QueryStatusComplete},
		pages: [][][]types.ResultField{
			{row("@message", "one", "@ptr", "p1"), row("@message", "two")},
			{row("@message", "three")},
		},
		stats: &types.QueryStatistics{RecordsMatched: 3, RecordsScanned: 40, BytesScanned: 5000},
	}
	runner := &Runner{Logs: logs}

	got, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	want := Result{
		Rows: [][]Field{
			{{Name: "@message", Value: "one"}, {Name: "@ptr", Value: "p1"}},
			{{Name: "@message", Value: "two"}},
			{{Name: "@message", Value: "three"}},
		},
		Stats: Stats{RecordsMatched: 3, RecordsScanned: 40, BytesScanned: 5000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if logs.polls != 3 {
		t.Fatalf("checked the status %d times, want 3", logs.polls)
	}
	if logs.stopped != nil {
		t.Fatalf("stopped a query that completed: %v", logs.stopped)
	}
}

func TestRunStartsTheQueryAsAsked(t *testing.T) {
	logs := &fakeLogs{statuses: []types.QueryStatus{types.QueryStatusComplete}, pages: [][][]types.ResultField{nil}}
	if _, err := (&Runner{Logs: logs}).Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	in := logs.started
	if !reflect.DeepEqual(in.LogGroupNames, []string{"app"}) || aws.ToString(in.QueryString) != "fields @message" {
		t.Fatalf("started with groups %v and query %q", in.LogGroupNames, aws.ToString(in.QueryString))
	}
	if aws.ToInt64(in.StartTime) != 1767607200 || aws.ToInt64(in.EndTime) != 1767610800 {
		t.Fatalf("started with %d to %d, want epoch seconds 1767607200 to 1767610800", aws.ToInt64(in.StartTime), aws.ToInt64(in.EndTime))
	}
}

func TestRunFailsWhenTheQueryDoesNotComplete(t *testing.T) {
	for _, status := range []types.QueryStatus{types.QueryStatusFailed, types.QueryStatusCancelled, types.QueryStatusTimeout, types.QueryStatusUnknown} {
		logs := &fakeLogs{statuses: []types.QueryStatus{types.QueryStatusRunning, status}}
		_, err := (&Runner{Logs: logs}).Run(context.Background(), request)
		if err == nil || !strings.Contains(err.Error(), string(status)) {
			t.Fatalf("status %s: got %v, want an error naming the status", status, err)
		}
	}
}

func TestCtrlCStopsTheQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logs := &fakeLogs{statuses: []types.QueryStatus{types.QueryStatusRunning}, cancel: cancel}

	_, err := (&Runner{Logs: logs, Poll: time.Hour}).Run(ctx, request)

	if !errors.Is(err, errInterrupted) {
		t.Fatalf("got %v, want interrupted", err)
	}
	if !reflect.DeepEqual(logs.stopped, []string{queryID}) {
		t.Fatalf("stopped %v, want %s", logs.stopped, queryID)
	}
	if logs.stopCtxErr != nil {
		t.Fatalf("StopQuery got a context that was already done: %v", logs.stopCtxErr)
	}
}

func TestFailedStatusCheckStopsTheQuery(t *testing.T) {
	logs := &fakeLogs{pollErr: errors.New("throttled")}

	_, err := (&Runner{Logs: logs}).Run(context.Background(), request)

	if err == nil || !strings.Contains(err.Error(), "throttled") {
		t.Fatalf("got %v, want the status check error", err)
	}
	if !reflect.DeepEqual(logs.stopped, []string{queryID}) {
		t.Fatalf("stopped %v, want %s", logs.stopped, queryID)
	}
}
