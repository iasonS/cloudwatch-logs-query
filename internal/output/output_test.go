package output

import (
	"strings"
	"testing"
	"time"

	"github.com/iasonS/cloudwatch-logs-query/internal/query"
)

func row(nameValues ...string) []query.Field {
	var fields []query.Field
	for i := 0; i < len(nameValues); i += 2 {
		fields = append(fields, query.Field{Name: nameValues[i], Value: nameValues[i+1]})
	}
	return fields
}

var logRows = [][]query.Field{
	row("@timestamp", "2026-01-05 10:00:00.123", "@message", "started", "@ptr", "p1"),
	row("@timestamp", "2026-01-05 10:00:01.000", "@message", "line one\nline two\n", "@ptr", "p2"),
}

func printed(t *testing.T, rows [][]query.Field, options Options) string {
	t.Helper()
	var out strings.Builder
	if err := Rows(&out, rows, options); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestTableIsAlignedSaysUTCAndHidesPtr(t *testing.T) {
	got := printed(t, logRows, Options{Format: Table})
	want := `@timestamp (UTC)         @message
2026-01-05 10:00:00.123  started
2026-01-05 10:00:01.000  line one\nline two
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPtrIsShownWhenAsked(t *testing.T) {
	got := printed(t, logRows, Options{Format: CSV, ShowPtr: true})
	want := "@timestamp,@message,@ptr\n" +
		"2026-01-05 10:00:00.123,started,p1\n" +
		"2026-01-05 10:00:01.000,\"line one\nline two\n\",p2\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestLocalConvertsTimesAndSaysSo(t *testing.T) {
	got := printed(t, logRows, Options{Format: Table, Local: time.FixedZone("CET", 3600)})
	for _, want := range []string{"@timestamp (local)", "2026-01-05 11:00:00.123 CET", "2026-01-05 11:00:01.000 CET"} {
		if !strings.Contains(got, want) {
			t.Fatalf("got\n%s\nwant it to contain %q", got, want)
		}
	}
}

func TestOnlyColumnsOfTimestampsAreLabelled(t *testing.T) {
	rows := [][]query.Field{
		row("bin(5m)", "2026-01-05 10:05:00.000", "when", "2026-01-05 10:00:00.000", "count()", "4"),
		row("bin(5m)", "2026-01-05 10:10:00.000", "when", "soon", "count()", "7"),
	}
	got := printed(t, rows, Options{Format: Table})
	header, _, _ := strings.Cut(got, "\n")
	if !strings.Contains(header, "bin(5m) (UTC)") || strings.Contains(header, "when (UTC)") || strings.Contains(header, "count() (UTC)") {
		t.Fatalf("got header %q, want only bin(5m) labelled UTC", header)
	}
}

func TestJSONPrintsOneObjectPerRow(t *testing.T) {
	rows := [][]query.Field{
		row("@timestamp", "2026-01-05 10:00:00.123", "@message", "a < b", "@ptr", "p1"),
		row("@message", "two"),
	}
	got := printed(t, rows, Options{Format: JSON})
	want := `{"@message":"a < b","@timestamp":"2026-01-05 10:00:00.123"}
{"@message":"two"}
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCSVLeavesMissingFieldsEmpty(t *testing.T) {
	rows := [][]query.Field{
		row("@message", "a, b"),
		row("@message", "c", "level", "warn"),
	}
	got := printed(t, rows, Options{Format: CSV})
	want := "@message,level\n\"a, b\",\nc,warn\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNoRowsPrintNothing(t *testing.T) {
	for _, format := range []Format{Table, JSON, CSV} {
		if got := printed(t, nil, Options{Format: format}); got != "" {
			t.Fatalf("%s printed %q for no rows", format, got)
		}
	}
}

func TestStatsLine(t *testing.T) {
	var out strings.Builder
	stats := query.Stats{RecordsMatched: 20, RecordsScanned: 18345, BytesScanned: 4567890}
	if err := Stats(&out, stats, 2140*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	want := "20 records matched, 18345 records scanned, 4.6 MB scanned, took 2.1s\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestReadableBytes(t *testing.T) {
	tests := map[float64]string{
		0:     "0 B",
		999:   "999 B",
		1500:  "1.5 kB",
		2.5e9: "2.5 GB",
		3e15:  "3000.0 TB",
	}
	for bytes, want := range tests {
		if got := readableBytes(bytes); got != want {
			t.Fatalf("%v bytes: got %q, want %q", bytes, got, want)
		}
	}
}
