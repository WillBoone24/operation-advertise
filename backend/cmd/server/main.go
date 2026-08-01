package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	corsware "github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"operation-advertise/backend/internal/auth"
	"operation-advertise/backend/internal/database"
	apihandlers "operation-advertise/backend/internal/handlers"
)

// ---------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------

// config holds everything main() needs to start the server, all
// sourced from environment variables. Collecting it into one struct
// (rather than scattering os.Getenv calls through main) gives us one
// place to see every knob this binary reads, and one place to add
// validation as the list grows.
type config struct {
	// Port the HTTP server listens on. Defaults to 8080.
	Port string

	// DBPath is the filesystem path to the SQLite database file.
	// Defaults to "storage/users.db", matching the project layout.
	DBPath string

	// JWTSecret signs and verifies auth tokens. Deliberately has NO
	// default — see loadConfig for why an unset secret is a hard
	// startup failure rather than a fallback value.
	JWTSecret string

	// AllowedOrigins is the set of frontend origins permitted to make
	// cross-origin requests (the resume portfolio site, and later the
	// RPG frontend). Comma-separated in the environment, e.g.
	// "https://example.com,https://rpg.example.com". Defaults to
	// allowing localhost dev servers only, so a forgotten env var in
	// production fails closed (broken CORS, loud and obvious) rather
	// than failing open (accidentally allowing every origin).
	AllowedOrigins []string
}

// defaultDevOrigins is the CORS fallback used only when
// ALLOWED_ORIGINS is not set. It covers common local dev server ports
// so `go run` works out of the box during development, without ever
// being a plausible production value — there's no world where
// "localhost:5173" is a legitimate production frontend origin, so this
// default can't accidentally leave production wide open.
var defaultDevOrigins = []string{
	"http://localhost:3000",
	"http://localhost:5173",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:5173",
}

// loadConfig reads configuration from environment variables and
// validates it. Returning an error (rather than log.Fatal-ing
// internally) keeps this function testable and keeps all
// fail-or-continue decisions visible in main().
func loadConfig() (config, error) {
	cfg := config{
		Port:   getEnvOrDefault("PORT", "8080"),
		DBPath: getEnvOrDefault("DB_PATH", "storage/users.db"),
	}

	// The JWT signing secret is the single most security-critical
	// piece of configuration in this service — anyone who has it can
	// forge valid tokens for any user. Unlike Port or DBPath, it gets
	// no convenience default. A missing secret must fail startup
	// loudly, not silently fall back to something guessable.
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	if cfg.JWTSecret == "" {
		return config{}, errors.New("JWT_SECRET environment variable must be set (see auth.NewTokenManager for minimum length requirements)")
	}

	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		origins := strings.Split(raw, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		cfg.AllowedOrigins = origins
	} else {
		cfg.AllowedOrigins = defaultDevOrigins
		log.Printf("main: ALLOWED_ORIGINS not set, defaulting to local dev origins: %v", defaultDevOrigins)
	}

	return cfg, nil
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------

// newRouter builds the full route table and returns it wrapped with
// CORS handling, ready to hand to http.Server. Kept as its own
// function (rather than inlined in main) so route wiring can be read
// top-to-bottom in one place, separate from process lifecycle concerns
// (signal handling, graceful shutdown) that live in main().
func newRouter(cfg config, db *database.DB, tm *auth.TokenManager) http.Handler {
	authHandler := apihandlers.NewAuthHandler(db, tm)
	profileHandler := apihandlers.NewProfileHandler(db)
	easterEggHandler := apihandlers.NewEasterEggHandler(db)
	gameHandler := apihandlers.NewGameHandler(db)
	boardHandler := apihandlers.NewBoardHandler(db)

	router := mux.NewRouter()

	// StrictSlash means /api/me and /api/me/ are treated as the same
	// route rather than one 404ing. Small ergonomics win, no downside
	// for an API with no nested resource paths yet.
	router.StrictSlash(true)

	api := router.PathPrefix("/api").Subrouter()

	// --- Public routes: no authentication required ---
	api.HandleFunc("/register", authHandler.Register).Methods(http.MethodPost)
	api.HandleFunc("/login", authHandler.Login).Methods(http.MethodPost)

	// --- Protected routes: require a valid JWT ---
	//
	// A dedicated subrouter with tm.Middleware applied via Use() means
	// "this route requires auth" is enforced structurally — a new
	// protected route is added here, not by remembering to wrap each
	// handler individually. It is impossible to add a route to this
	// subrouter and forget the auth check.
	protected := api.PathPrefix("").Subrouter()
	protected.Use(tm.Middleware)
	protected.HandleFunc("/me", profileHandler.Me).Methods(http.MethodGet)
	protected.HandleFunc("/easteregg", easterEggHandler.Found).Methods(http.MethodPost)
	protected.HandleFunc("/game/create", gameHandler.CreateCharacter).Methods(http.MethodPost)
	protected.HandleFunc("/game/state", gameHandler.GetState).Methods(http.MethodGet)
	protected.HandleFunc("/game/action", gameHandler.Action).Methods(http.MethodPost)
	protected.HandleFunc("/game/board", boardHandler.ListNotes).Methods(http.MethodGet)
	protected.HandleFunc("/game/board", boardHandler.PostNote).Methods(http.MethodPost)

	// health check: unauthenticated, outside /api, for load balancers
	// / uptime monitors / a quick curl to confirm the process is alive.
	// Deliberately returns no information beyond "the process is up
	// and can write an HTTP response" — it does not touch the
	// database, so it can't be used to infer DB health, which is a
	// separate concern this endpoint isn't trying to solve.
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}).Methods(http.MethodGet)

	corsMiddleware := corsware.CORS(
		corsware.AllowedOrigins(cfg.AllowedOrigins),
		corsware.AllowedMethods([]string{http.MethodGet, http.MethodPost, http.MethodOptions}),
		corsware.AllowedHeaders([]string{"Content-Type", "Authorization"}),
		// AllowCredentials is intentionally omitted. This API uses
		// bearer tokens in the Authorization header (see
		// auth/middleware.go), not cookies, so there is no session
		// cookie that needs credentialed cross-origin requests.
		// Omitting it keeps the CORS policy as narrow as the actual
		// auth mechanism requires.
	)

	return corsMiddleware(router)
}

// ---------------------------------------------------------------------
// main
// ---------------------------------------------------------------------

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("main: configuration error: %v", err)
	}

	tokenManager, err := auth.NewTokenManager(cfg.JWTSecret)
	if err != nil {
		// Fails fast on a too-short/empty secret rather than starting
		// a server that would issue insecure tokens. See
		// auth.NewTokenManager's own validation for the specifics.
		log.Fatalf("main: failed to initialize token manager: %v", err)
	}

	db, err := database.New(database.Config{Path: cfg.DBPath})
	if err != nil {
		log.Fatalf("main: failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		log.Fatalf("main: failed to run migrations: %v", err)
	}

	router := newRouter(cfg, db, tokenManager)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
		// Explicit timeouts rather than the net/http zero-value
		// (no timeout). An API with no read/write timeout is
		// vulnerable to slow-client resource exhaustion (a client
		// that opens a connection and trickles bytes forever ties up
		// a goroutine indefinitely). These values are generous for a
		// JSON API with no file uploads and no long-polling routes.
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run the server in a goroutine so main() is free to block on the
	// shutdown signal below. ListenAndServe only ever returns an
	// error (it blocks on success), and http.ErrServerClosed is the
	// expected, non-error return value produced by Shutdown() below —
	// anything else is a genuine startup/runtime failure.
	go func() {
		log.Printf("main: listening on :%s (db=%s)", cfg.Port, cfg.DBPath)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("main: server error: %v", err)
		}
	}()

	// Block until SIGINT (Ctrl+C) or SIGTERM (e.g. from `docker stop`
	// or a process manager) is received, then shut down gracefully
	// rather than dropping in-flight connections. This matters even
	// for a portfolio project: SQLite's single-writer model (see
	// database.go's MaxOpenConns(1) comment) means an abrupt kill
	// mid-write is exactly the kind of thing that can leave the DB in
	// an inconsistent state.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("main: shutdown signal received, draining connections")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("main: graceful shutdown failed: %v", err)
	} else {
		log.Println("main: shutdown complete")
	}
}