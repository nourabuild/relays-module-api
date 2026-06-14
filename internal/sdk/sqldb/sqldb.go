// Package sqldb provides database operations for the relays service.
package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nourabuild/relays-api/internal/sdk/config"
	"github.com/nourabuild/relays-api/internal/sdk/models"
)

// lib/pq errorCodeNames
// https://github.com/lib/pq/blob/master/error.go#L178
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
	notNullViolation    = "23502"
)

var (
	ErrDBNotFound          = sql.ErrNoRows
	ErrDBDuplicatedEntry   = errors.New("duplicated entry")
	ErrForeignKeyViolation = errors.New("foreign key violation")
	ErrCheckViolation      = errors.New("check constraint violation")
	ErrNotNullViolation    = errors.New("not null violation")
)

// Service represents a service that interacts with a database.
type Service interface {
	// Health returns a map of health status information.
	// The keys and values in the map are service-specific.
	Health() map[string]string

	// Close terminates the database connection.
	// It returns an error if the connection cannot be closed.
	Close() error

	// User operations
	GetUserByID(ctx context.Context, userID string) (models.User, error)
	GetUserByEmail(ctx context.Context, email string) (models.User, error)
	GetUserByAccount(ctx context.Context, account string) (models.User, error)
	CreateUser(ctx context.Context, user models.NewUser) (models.User, error)
	ListUsers(ctx context.Context) ([]models.User, error)
	SearchUsers(ctx context.Context, query string) ([]models.User, error)

	// Task operations
	ListExpectations(ctx context.Context, userID string) ([]models.Task, error)
	ListTodos(ctx context.Context, userID string) ([]models.Task, error)
	CreateTask(ctx context.Context, creatorID string, input models.CreateTask) (models.Task, error)
	GetTask(ctx context.Context, taskID string) (models.Task, error)
	UpdateTask(ctx context.Context, taskID string, input models.UpdateTask) (models.Task, error)
	UpdateTaskStatus(ctx context.Context, taskID, status string) (models.Task, error)
	CreateTaskMessage(ctx context.Context, taskID, authorID string, input models.CreateTaskMessage) (models.TaskMessage, error)
	ListTaskMessages(ctx context.Context, taskID, userID string) ([]models.TaskMessage, error)
}

type service struct {
	db       *sql.DB
	database string
}

func New(cfg config.DB) (Service, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&search_path=%s",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode, cfg.Schema)
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Bound the pool: the driver default is unlimited open connections.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &service{
		db:       db,
		database: cfg.Database,
	}, nil
}

// Health checks the health of the database connection by pinging the database.
// It returns a map with keys indicating various health statistics.
func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := make(map[string]string)
	const (
		openConnectionsWarn = 40
		waitCountWarn       = 1000
	)

	// Ping the database
	err := s.db.PingContext(ctx)
	if err != nil {
		stats["status"] = "down"
		stats["error"] = fmt.Sprintf("db down: %v", err)
		log.Printf("db down: %v", err)
		return stats
	}

	// Database is up, add more statistics
	stats["status"] = "up"
	stats["message"] = "It's healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := s.db.Stats()
	stats["open_connections"] = strconv.Itoa(dbStats.OpenConnections)
	stats["in_use"] = strconv.Itoa(dbStats.InUse)
	stats["idle"] = strconv.Itoa(dbStats.Idle)
	stats["wait_count"] = strconv.FormatInt(dbStats.WaitCount, 10)
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = strconv.FormatInt(dbStats.MaxIdleClosed, 10)
	stats["max_lifetime_closed"] = strconv.FormatInt(dbStats.MaxLifetimeClosed, 10)

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > openConnectionsWarn {
		stats["message"] = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > waitCountWarn {
		stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats
}

// Close closes the database connection.
// It logs a message indicating the disconnection from the specific database.
// If the connection is successfully closed, it returns nil.
// If an error occurs while closing the connection, it returns the error.
func (s *service) Close() error {
	log.Printf("Disconnected from database: %s", s.database)
	return s.db.Close()
}

// ---------------------------------------------
// SQL Commands
// ---------------------------------------------

// GetUserByID retrieves a user by their ID.
func (s *service) GetUserByID(ctx context.Context, userID string) (models.User, error) {
	const query = `
		SELECT
			id::text,
			name,
			account,
			email,
			bio,
			dob,
			city,
			phone,
			avatar_photo_id,
			is_admin,
			created_at,
			updated_at
		FROM todos.users
		WHERE id = $1
	`

	user, err := scanUser(s.db.QueryRowContext(ctx, query, userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, ErrDBNotFound
		}
		return models.User{}, fmt.Errorf("selecting user: %w", err)
	}

	return user, nil
}

// GetUserByEmail retrieves a user by their email address.
func (s *service) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	const query = `
		SELECT
			id::text,
			name,
			account,
			email,
			bio,
			dob,
			city,
			phone,
			avatar_photo_id,
			is_admin,
			created_at,
			updated_at
		FROM todos.users
		WHERE email = $1
	`

	user, err := scanUser(s.db.QueryRowContext(ctx, query, email))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, ErrDBNotFound
		}
		return models.User{}, fmt.Errorf("selecting user by email: %w", err)
	}

	return user, nil
}

// GetUserByAccount retrieves a user by their account name.
func (s *service) GetUserByAccount(ctx context.Context, account string) (models.User, error) {
	const query = `
		SELECT
			id::text,
			name,
			account,
			email,
			bio,
			dob,
			city,
			phone,
			avatar_photo_id,
			is_admin,
			created_at,
			updated_at
		FROM todos.users
		WHERE account = $1
	`

	user, err := scanUser(s.db.QueryRowContext(ctx, query, account))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, ErrDBNotFound
		}
		return models.User{}, fmt.Errorf("selecting user by account: %w", err)
	}

	return user, nil
}

// CreateUser inserts a new user into the database.
func (s *service) CreateUser(ctx context.Context, newUser models.NewUser) (models.User, error) {
	const query = `
		INSERT INTO todos.users (id, name, account, email, is_admin)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, name, account, email, bio, dob, city, phone, avatar_photo_id, is_admin, created_at, updated_at
	`

	user, err := scanUser(s.db.QueryRowContext(ctx, query,
		newUser.ID,
		newUser.Name,
		newUser.Account,
		newUser.Email,
		false, // is_admin defaults to false
	))

	if err != nil {
		if isPgError(err, uniqueViolation) {
			return models.User{}, ErrDBDuplicatedEntry
		}
		return models.User{}, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

// ListUsers retrieves all users from the database.
func (s *service) ListUsers(ctx context.Context) ([]models.User, error) {
	const query = `
		SELECT
			id::text,
			name,
			account,
			email,
			bio,
			dob,
			city,
			phone,
			avatar_photo_id,
			is_admin,
			created_at,
			updated_at
		FROM todos.users
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users: %w", err)
	}

	return users, nil
}

// SearchUsers searches for users by name, email, or account using ILIKE.
func (s *service) SearchUsers(ctx context.Context, query string) ([]models.User, error) {
	const sqlQuery = `
		SELECT
			id::text,
			name,
			account,
			email,
			bio,
			dob,
			city,
			phone,
			avatar_photo_id,
			is_admin,
			created_at,
			updated_at
		FROM todos.users
		WHERE name ILIKE $1 OR email ILIKE $1 OR account ILIKE $1
		ORDER BY created_at DESC
	`

	searchPattern := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx, sqlQuery, searchPattern)
	if err != nil {
		return nil, fmt.Errorf("searching users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating search results: %w", err)
	}

	return users, nil
}

// ---------------------------------------------
// Helpers
// ---------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(scanner rowScanner) (models.User, error) {
	var user models.User
	var bio, dob, city, phone sql.NullString
	var avatarPhotoID sql.NullInt32
	if err := scanner.Scan(
		&user.ID,
		&user.Name,
		&user.Account,
		&user.Email,
		&bio,
		&dob,
		&city,
		&phone,
		&avatarPhotoID,
		&user.IsAdmin,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		return models.User{}, err
	}

	user.Bio = StringPtr(bio)
	user.DOB = StringPtr(dob)
	user.City = StringPtr(city)
	user.Phone = StringPtr(phone)
	user.AvatarPhotoID = Int32Ptr(avatarPhotoID)

	return user, nil
}

// isPgError checks if the error is a PostgreSQL error with the given code.
func isPgError(err error, code string) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == code
	}
	return false
}

// NullString creates a sql.NullString from a string pointer.
func NullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// NullTime creates a sql.NullTime from a time.Time pointer.
func NullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// StringPtr returns a pointer to a string from sql.NullString.
func StringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// Int32Ptr returns a pointer to an int from sql.NullInt32.
func Int32Ptr(ni sql.NullInt32) *int {
	if !ni.Valid {
		return nil
	}
	intVal := int(ni.Int32)
	return &intVal
}

// TimePtr returns a pointer to a time.Time from sql.NullTime.
func TimePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}
