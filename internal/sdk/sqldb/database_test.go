package sqldb

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nourabuild/relays-api/internal/sdk/config"
)

// testDBConfig points at the throwaway Postgres container started in
// TestMain; every test builds its service from it.
var testDBConfig config.DB

func mustStartPostgresContainer() (func(context.Context) error, error) {
	var (
		dbName = "database"
		dbPwd  = "password"
		dbUser = "user"
	)

	dbContainer, err := postgres.Run(
		context.Background(),
		"postgres:latest",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPwd),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		return nil, err
	}

	terminate := func(ctx context.Context) error {
		return dbContainer.Terminate(ctx)
	}

	dbHost, err := dbContainer.Host(context.Background())
	if err != nil {
		return terminate, err
	}

	dbPort, err := dbContainer.MappedPort(context.Background(), "5432/tcp")
	if err != nil {
		return terminate, err
	}

	testDBConfig = config.DB{
		Host:     dbHost,
		Port:     dbPort.Port(),
		Username: dbUser,
		Password: dbPwd,
		Database: dbName,
		Schema:   "todos",
		SSLMode:  "disable", // test container has no TLS
	}

	return terminate, nil
}

// runMigrations applies every *.up.sql migration in order against the test
// container, so integration tests run against the real schema.
func runMigrations(cfg config.DB) error {
	// simple_protocol allows multi-statement migration files through pgx.
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&default_query_exec_mode=simple_protocol",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode)
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("opening migration connection: %w", err)
	}
	defer db.Close()

	pattern := filepath.Join("..", "migrate", "sql", "*.up.sql")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("globbing migrations: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no migrations found at %s", pattern)
	}
	sort.Strings(files)

	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			return fmt.Errorf("applying %s: %w", filepath.Base(file), err)
		}
	}

	return nil
}

func newTestService(t *testing.T) Service {
	t.Helper()

	srv, err := New(testDBConfig)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func TestMain(m *testing.M) {
	if os.Getenv("SHORT") != "" {
		os.Exit(0)
	}

	teardown, err := mustStartPostgresContainer()
	if err != nil {
		log.Fatalf("could not start postgres container: %v", err)
	}

	if err := runMigrations(testDBConfig); err != nil {
		if teardown != nil {
			_ = teardown(context.Background())
		}
		log.Fatalf("could not run migrations: %v", err)
	}

	code := m.Run()

	if teardown != nil {
		if err := teardown(context.Background()); err != nil {
			log.Fatalf("could not teardown postgres container: %v", err)
		}
	}

	os.Exit(code)
}

func TestNew(t *testing.T) {
	if srv := newTestService(t); srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHealth(t *testing.T) {
	srv := newTestService(t)

	stats := srv.Health()

	if stats["status"] != "up" {
		t.Fatalf("expected status to be up, got %s", stats["status"])
	}

	if _, ok := stats["error"]; ok {
		t.Fatalf("expected error not to be present")
	}

	if !strings.Contains(stats["message"], "healthy") {
		t.Fatalf("expected healthy message, got %s", stats["message"])
	}
}

func TestClose(t *testing.T) {
	srv, err := New(testDBConfig)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	if srv.Close() != nil {
		t.Fatalf("expected Close() to return nil")
	}
}
