package app

import (
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
	"github.com/nourabuild/relays-api/internal/services/jwt"
	"github.com/nourabuild/relays-api/internal/services/mailtrap"
	"github.com/nourabuild/relays-api/internal/services/sentry"
	"github.com/nourabuild/relays-api/internal/services/websocket"
)

type App struct {
	db       sqldb.Service
	sentry   *sentry.SentryService
	jwt      *jwt.TokenService
	mailtrap *mailtrap.MailtrapService
	ws       *websocket.Service
}

func NewApp(
	db sqldb.Service,
	sentry *sentry.SentryService,
	jwt *jwt.TokenService,
	mailtrap *mailtrap.MailtrapService,
) *App {
	return &App{
		db:       db,
		sentry:   sentry,
		jwt:      jwt,
		mailtrap: mailtrap,
		ws:       websocket.NewService(),
	}
}
