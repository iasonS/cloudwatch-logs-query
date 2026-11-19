// Package output prints query rows as an aligned table, JSON lines or CSV, and the line of
// query statistics.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iasonS/cwq/internal/query"
)

// Format is how rows are printed.
type Format string

const (
	Table Format = "table"
	JSON  Format = "json"
	CSV   Format = "csv"
)

// insightsTime is how Logs Insights returns timestamps, in UTC. Parsing also accepts the
// fraction of a second that follows.
const insightsTime = "2006-01-02 15:04:05"

const localTime = "2006-01-02 15:04:05.000 MST"

// ptrField is the record id Logs Insights adds to every row.
const ptrField = "@ptr"

// oneLine keeps a table row on one line when a value holds newlines or tabs, such as a
// stack trace.
var oneLine = strings.NewReplacer("\r", `\r`, "\n", `\n`, "\t", `\t`)

// Options say how to print rows.
type Options struct {
	Format  Format
	ShowPtr bool           // keep the @ptr field
	Local   *time.Location // when set, timestamps are converted to this zone
}

// Rows prints the rows of a query result. No rows print nothing.
func Rows(w io.Writer, rows [][]query.Field, options Options) error {
	if len(rows) == 0 {
		return nil
	}
	if !options.ShowPtr {
		rows = withoutField(rows, ptrField)
	}
	columns := columnsOf(rows)
	times := timeColumns(rows, columns)
	if options.Local != nil {
		rows = inZone(rows, times, options.Local)
	}

	switch options.Format {
	case JSON:
		return writeJSON(w, rows)
	case CSV:
		return writeCSV(w, columns, rows)
	}
	return writeTable(w, header(columns, times, options.Local), columns, rows)
}

// Stats prints what the query scanned and how long cwq took. Logs Insights bills by the
// bytes scanned.
func Stats(w io.Writer, stats query.Stats, elapsed time.Duration) error {
	_, err := fmt.Fprintf(w, "%.0f records matched, %.0f records scanned, %s scanned, took %s\n",
		stats.RecordsMatched, stats.RecordsScanned, readableBytes(stats.BytesScanned), elapsed.Round(100*time.Millisecond))
	return err
}

func readableBytes(bytes float64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	unit := 0
	for bytes >= 1000 && unit < len(units)-1 {
		bytes /= 1000
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f B", bytes)
	}
	return fmt.Sprintf("%.1f %s", bytes, units[unit])
}

func withoutField(rows [][]query.Field, name string) [][]query.Field {
	kept := make([][]query.Field, 0, len(rows))
	for _, row := range rows {
		var fields []query.Field
		for _, field := range row {
			if field.Name != name {
				fields = append(fields, field)
			}
		}
		kept = append(kept, fields)
	}
	return kept
}

// columnsOf lists every field name in the order it first appears.
func columnsOf(rows [][]query.Field) []string {
	var columns []string
	for _, row := range rows {
		for _, field := range row {
			if !slices.Contains(columns, field.Name) {
				columns = append(columns, field.Name)
			}
		}
	}
	return columns
}

// timeColumns finds the columns whose values are all Logs Insights timestamps, such as
// @timestamp or bin(5m).
func timeColumns(rows [][]query.Field, columns []string) map[string]bool {
	times := map[string]bool{}
	for _, column := range columns {
		times[column] = allTimes(rows, column)
	}
	return times
}

func allTimes(rows [][]query.Field, column string) bool {
	found := false
	for _, row := range rows {
		value := valueOf(row, column)
		if value == "" {
			continue
		}
		if _, err := time.Parse(insightsTime, value); err != nil {
			return false
		}
		found = true
	}
	return found
}

func valueOf(row []query.Field, name string) string {
	for _, field := range row {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}

func inZone(rows [][]query.Field, times map[string]bool, zone *time.Location) [][]query.Field {
	converted := make([][]query.Field, 0, len(rows))
	for _, row := range rows {
		fields := make([]query.Field, 0, len(row))
		for _, field := range row {
			if times[field.Name] {
				field.Value = toZone(field.Value, zone)
			}
			fields = append(fields, field)
		}
		converted = append(converted, fields)
	}
	return converted
}

func toZone(value string, zone *time.Location) string {
	utc, err := time.Parse(insightsTime, value)
	if err != nil {
		return value
	}
	return utc.In(zone).Format(localTime)
}

// header names the columns and says which zone the timestamp columns are in.
func header(columns []string, times map[string]bool, local *time.Location) []string {
	zone := "UTC"
	if local != nil {
		zone = "local"
	}
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column
		if times[column] {
			names[i] = column + " (" + zone + ")"
		}
	}
	return names
}

func cells(row []query.Field, columns []string) []string {
	values := make([]string, len(columns))
	for i, column := range columns {
		values[i] = valueOf(row, column)
	}
	return values
}

func writeTable(w io.Writer, header, columns []string, rows [][]query.Field) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, strings.Join(header, "\t"))
	for _, row := range rows {
		values := cells(row, columns)
		for i, value := range values {
			values[i] = tableCell(value)
		}
		fmt.Fprintln(table, strings.Join(values, "\t"))
	}
	return table.Flush()
}

func tableCell(value string) string {
	return oneLine.Replace(strings.TrimRight(value, "\r\n"))
}

// writeJSON prints one object per row, with the fields that row has. Keys come out sorted.
func writeJSON(w io.Writer, rows [][]query.Field) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, row := range rows {
		object := map[string]string{}
		for _, field := range row {
			object[field.Name] = field.Value
		}
		if err := encoder.Encode(object); err != nil {
			return err
		}
	}
	return nil
}

func writeCSV(w io.Writer, columns []string, rows [][]query.Field) error {
	out := csv.NewWriter(w)
	if err := out.Write(columns); err != nil {
		return err
	}
	for _, row := range rows {
		if err := out.Write(cells(row, columns)); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
