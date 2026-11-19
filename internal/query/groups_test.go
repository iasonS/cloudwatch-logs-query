package query

import (
	"context"
	"reflect"
	"testing"
)

func TestGroupsAddsEveryGroupWithThePrefixOnce(t *testing.T) {
	logs := &fakeLogs{groups: []string{"/svc/api", "/svc/worker", "/other/api"}}
	runner := &Runner{Logs: logs}

	got, err := runner.Groups(context.Background(), []string{"/svc/api", "/extra"}, []string{"/svc/"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/extra", "/svc/api", "/svc/worker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPrefixThatMatchesNothingIsAnError(t *testing.T) {
	runner := &Runner{Logs: &fakeLogs{groups: []string{"/svc/api"}}}
	if _, err := runner.Groups(context.Background(), nil, []string{"/nothing/"}); err == nil {
		t.Fatal("a prefix that matches no log group was accepted")
	}
}
