package app

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"time"

	identityapp "example.com/phan-quyen-golang/internal/identity/application"
	identitydelivery "example.com/phan-quyen-golang/internal/identity/delivery"
	identityinfra "example.com/phan-quyen-golang/internal/identity/infra"
	invoicapp "example.com/phan-quyen-golang/internal/invoice/application"
	invoicedelivery "example.com/phan-quyen-golang/internal/invoice/delivery"
	invoiceinfra "example.com/phan-quyen-golang/internal/invoice/infra"
	membershipapp "example.com/phan-quyen-golang/internal/membership/application"
	membershipdelivery "example.com/phan-quyen-golang/internal/membership/delivery"
	membershipinfra "example.com/phan-quyen-golang/internal/membership/infra"
	securityapp "example.com/phan-quyen-golang/internal/security/application"
	securitydelivery "example.com/phan-quyen-golang/internal/security/delivery"
	securityinfra "example.com/phan-quyen-golang/internal/security/infra"
	"example.com/phan-quyen-golang/internal/shared/config"
	"example.com/phan-quyen-golang/internal/shared/database/sqlite"
	"github.com/gin-gonic/gin"
)

type App struct {
	database *sql.DB
	router   http.Handler
}

func New(cfg config.Config) (*App, error) {
	db, err := sqlite.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err := Migrate(db); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	repository := securityinfra.NewRepository(db)
	resolver := securityapp.NewEndpointResolver(repository,
		map[string]securityapp.ResourceLoader{
			"me":                   identitydelivery.MeLoader{},
			"invoice":              invoicedelivery.NewResourceLoader(db),
			"membership-apply":     membershipdelivery.ApplyLoader{},
			"membership-review":    membershipdelivery.NewReviewLoader(db),
			"membership-invite":    membershipdelivery.InviteLoader{},
			"membership-accept":    membershipdelivery.AcceptLoader{},
			"external-grant-owner": securitydelivery.ExternalGrantOwnerLoader{},
		},
		map[string]securityapp.IntentResolver{
			"me-read":               identitydelivery.MeIntent{},
			"approve":               invoicedelivery.ApproveIntent{},
			"membership-apply":      membershipdelivery.ApplyIntent{},
			"membership-review":     membershipdelivery.ReviewIntent{},
			"membership-invite":     membershipdelivery.InviteIntent{},
			"membership-accept":     membershipdelivery.AcceptIntent{},
			"external-grant-manage": securitydelivery.ExternalGrantManageIntent{},
		},
	)
	authentication := securitydelivery.NewAuthentication(securitydelivery.NewVerifier(cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTSecret), repository)
	authorizer := securityapp.NewEngine(securityapp.NewHardEngine(repository), securityapp.NewSoftEngine(repository, repository))
	authorization := securitydelivery.NewAuthorization(authorizer, repository)
	store := invoiceinfra.NewStore(db)
	handler := invoicedelivery.NewHandler(invoicapp.NewApproveService(store, store, store))
	membershipRepository := membershipinfra.NewRepository(db)
	membershipHandler := membershipdelivery.NewHandler(membershipapp.NewService(membershipRepository), membershipapp.NewInvitationService(membershipRepository))
	externalGrantHandler := securitydelivery.NewExternalGrantHandler(securityapp.NewExternalGrantService(repository, repository))
	identityHandler := identitydelivery.NewHandler(identityapp.NewService(identityinfra.NewRepository(db)))
	router := gin.New()
	router.Use(gin.Recovery())
	protected := router.Group("/v1")
	protected.Use(authentication.Middleware(), securitydelivery.NewEndpointContext(resolver, repository).Middleware())
	invoicedelivery.RegisterRoutes(protected, invoicedelivery.RouteDependencies{Guard: authorization.Enforce, Handler: handler})
	membershipdelivery.RegisterRoutes(protected, membershipdelivery.RouteDependencies{Guard: authorization.Enforce, Handler: membershipHandler})
	securitydelivery.RegisterExternalGrantRoutes(protected, authorization.Enforce, externalGrantHandler)
	identitydelivery.RegisterRoutes(protected, authorization.Enforce, identityHandler)
	return &App{database: db, router: router}, nil
}

func (a *App) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	a.router.ServeHTTP(writer, request)
}
func (a *App) Close() error      { return a.database.Close() }
func (a *App) Database() *sql.DB { return a.database }

// Run starts the HTTP application and shuts it down when the context is canceled.
func Run(ctx context.Context, cfg config.Config) (runErr error) {
	application, err := New(cfg)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, application.Close())
	}()
	server := &http.Server{Addr: cfg.HTTPAddress, Handler: application.router, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return err
	}
	return serve(ctx, server, listener)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-finished
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
