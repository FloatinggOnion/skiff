package panel

import (
	"reflect"
	"testing"

	"github.com/nuelScript/skiff/internal/config"
)

// filled sets every exported field to a value that isn't its zero, so a field the spec forgets shows up as lost.
func filled(t *testing.T, v any, special map[string]any) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	for i := range rv.NumField() {
		f, field := rv.Field(i), rv.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		if s, ok := special[field.Name]; ok {
			f.Set(reflect.ValueOf(s))
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(field.Name + "-value")
		case reflect.Int, reflect.Int64:
			f.SetInt(int64(i + 7))
		case reflect.Bool:
			f.SetBool(true)
		default:
			t.Fatalf("filled doesn't know how to set %s (%s)", field.Name, f.Kind())
		}
	}
}

func TestSpecKeepsEveryAppSetting(t *testing.T) {
	var s Source
	filled(t, &s, map[string]any{
		"Port":       "8080",
		"Origin":     originCLI,
		"RunOptions": config.EncodeRunOptions(config.RunOptions{User: "app", WorkingDir: "/srv", Init: true}),
	})
	if got := specSource(sourceSpec(s)); !reflect.DeepEqual(got, s) {
		t.Errorf("an app lost settings on its way through the spec:\n got %+v\nwant %+v", got, s)
	}
	var empty Source
	if got := specSource(sourceSpec(empty)); !reflect.DeepEqual(got, empty) {
		t.Errorf("an empty app gained settings: %+v", got)
	}
}

func TestSpecKeepsWorkersAndJobs(t *testing.T) {
	var w workerRow
	filled(t, &w, nil)
	if got := specWorker(workerSpec(w)); got != w {
		t.Errorf("worker: got %+v, want %+v", got, w)
	}
	var j jobRow
	filled(t, &j, nil)
	want := j
	want.LastRun, want.LastOk = 0, 0 // what happened isn't part of what was asked for
	if got := specJob(jobSpec(j)); got != want {
		t.Errorf("job: got %+v, want %+v", got, want)
	}
}
