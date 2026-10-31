package panel

import (
	"testing"

	"github.com/nuelScript/skiff/internal/docker"
)

func TestEngineForImage(t *testing.T) {
	cases := map[string]string{
		"postgres:16-alpine":                "postgres",
		"docker.io/library/postgres:15":     "postgres",
		"postgis/postgis:16-3.4":            "postgres",
		"mysql:8":                           "mysql",
		"mongo@sha256:abc":                  "mongodb",
		"localhost:5000/redis:7":            "redis",
		"registry.example.com:5000/nginx:1": "",
		"skiff-tribe-ng:latest":             "",
	}
	for in, want := range cases {
		if got := engineForImage(in); got != want {
			t.Errorf("engineForImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCredsFrom(t *testing.T) {
	pg, ok := credsFrom("postgres", docker.ContainerRuntime{Env: map[string]string{"POSTGRES_PASSWORD": "pw"}})
	if !ok || pg != (dbCreds{user: "postgres", pass: "pw", dbname: "postgres"}) {
		t.Errorf("postgres defaults: %+v", pg)
	}
	pg, _ = credsFrom("postgres", docker.ContainerRuntime{Env: map[string]string{"POSTGRES_USER": "app", "POSTGRES_PASSWORD": "pw", "POSTGRES_DB": "tribe"}})
	if pg != (dbCreds{user: "app", pass: "pw", dbname: "tribe"}) {
		t.Errorf("postgres explicit: %+v", pg)
	}
	if my, ok := credsFrom("mysql", docker.ContainerRuntime{Env: map[string]string{"MYSQL_ROOT_PASSWORD": "r", "MYSQL_DATABASE": "d"}}); !ok || my.user != "root" {
		t.Errorf("mysql root fallback: %+v", my)
	}
	if _, ok := credsFrom("mongodb", docker.ContainerRuntime{Env: map[string]string{}}); ok {
		t.Error("mongo without root credentials should not be adoptable")
	}
	rd, ok := credsFrom("redis", docker.ContainerRuntime{Cmd: []string{"redis-server", "--requirepass", "s3cret"}})
	if !ok || rd.pass != "s3cret" {
		t.Errorf("redis: %+v", rd)
	}
}
