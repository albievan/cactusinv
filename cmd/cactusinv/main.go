// Command cactusinv is the composition root: the only place adapters are wired to the
// application. It serves the API and web app and runs the background workers.
//
//	cactusinv [serve]                    run the server (default)
//	cactusinv migrate up|down            apply or roll back database migrations
//	cactusinv admin create <login>       create an administrator (password on stdin)
//	cactusinv admin set-password <login> reset a password (password on stdin)
//	cactusinv backup keygen|create|list|restore|note-legacy-restore
//	                                     backups and restores (cactusinv backup for help)
//	cactusinv healthcheck                exit 0 if the local server is healthy (Docker)
//	cactusinv version
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the distroless image has no zoneinfo

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/albievan/cactusinv/internal/adapters/aesgcm"
	"github.com/albievan/cactusinv/internal/adapters/archive"
	"github.com/albievan/cactusinv/internal/adapters/argon2"
	"github.com/albievan/cactusinv/internal/adapters/clock"
	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/adapters/idgen"
	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/adapters/pdf"
	"github.com/albievan/cactusinv/internal/adapters/smtp"
	"github.com/albievan/cactusinv/internal/adapters/worker"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/web"
)

// version and build are set at build time:
// -ldflags "-X main.version=1.2.0 -X main.build=42".
var (
	version = "dev"
	build   = "0"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cactusinv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version":
		fmt.Println(version + " (build " + build + ")")
		return nil
	case "healthcheck":
		return healthcheck()
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		return serve(ctx, cfg, log)
	case "migrate":
		if len(args) < 2 || (args[1] != "up" && args[1] != "down") {
			return errors.New("usage: cactusinv migrate up|down")
		}
		db, err := openDB(ctx, cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		return mariadb.Migrate(ctx, db, args[1] == "down")
	case "admin":
		return adminCommand(ctx, cfg, args[1:])
	case "backup":
		st, _ := os.Stdin.Stat()
		return backupCommand(ctx, cfg, args[1:], os.Stdin, os.Stdout, st != nil && st.Mode()&os.ModeCharDevice != 0)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

// dbConfig locates the live database (the password comes from the secrets).
func dbConfig(ctx context.Context, cfg config) (mariadb.Config, error) {
	pw, err := cfg.secret(ctx, "db_password")
	if err != nil {
		return mariadb.Config{}, err
	}
	return mariadb.Config{Host: cfg.DBHost, Port: cfg.DBPort, Name: cfg.DBName, User: cfg.DBUser, Password: pw}, nil
}

func openDB(ctx context.Context, cfg config) (*mariadbDB, error) {
	dc, err := dbConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var db *mariadbDB
	// MariaDB may still be starting when the container comes up: retry for a while.
	deadline := time.Now().Add(cfg.DBWait)
	for {
		db, err = mariadb.Open(ctx, dc)
		if err == nil || time.Now().After(deadline) || ctx.Err() != nil {
			return db, err
		}
		time.Sleep(2 * time.Second)
	}
}

// newService builds the application; the returned func releases what it opened.
func newService(ctx context.Context, cfg config, db *mariadbDB, log *slog.Logger) (*app.Service, func(), error) {
	deps := app.Deps{
		Tx: mariadb.NewStore(db), Clock: clock.System{}, IDs: idgen.UUIDv7{}, Renderer: pdf.Renderer{},
		Hasher: argon2.New(argon2.DefaultConcurrency), Logger: log, Archive: archive.Dir{Root: cfg.InvoicesDir},
	}
	dc, err := dbConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	backups, closeBackups, err := backupDeps(ctx, cfg, db, dc, log)
	if err != nil {
		return nil, nil, err
	}
	deps.Backups = backups
	// app_key encrypts the SMTP password saved in Settings → E-mail server.
	if key, err := cfg.optionalSecret(ctx, "app_key"); err != nil {
		closeBackups()
		return nil, nil, err
	} else if key != "" {
		c, err := aesgcm.New(key)
		if err != nil {
			closeBackups()
			return nil, nil, err
		}
		deps.Cipher = c
	} else {
		log.Warn("no app_key secret: the SMTP password cannot be saved in the app")
	}
	// The deployment's server (.env) is the fallback while none is configured in the app.
	var fallback ports.Mailer = noMailer{}
	if cfg.SMTPHost != "" {
		pw, err := cfg.optionalSecret(ctx, "smtp_password")
		if err != nil {
			closeBackups()
			return nil, nil, err
		}
		m, err := smtp.New(smtp.Config{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUser, Password: pw, From: cfg.SMTPFrom, TLS: cfg.SMTPTLS})
		if err != nil {
			closeBackups()
			return nil, nil, err
		}
		fallback = m
	}
	var svc *app.Service
	deps.Mailer = smtp.Dynamic{Fallback: fallback, Resolve: func(ctx context.Context) (smtp.Config, bool, error) {
		st, pw, ok, err := svc.SMTPServer(ctx)
		if err != nil || !ok {
			return smtp.Config{}, false, err
		}
		return smtp.Config{Host: st.Host, Port: st.Port, Username: st.Username, Password: pw, From: st.From, TLS: st.TLS}, true, nil
	}}
	svc = app.New(deps)
	return svc, closeBackups, nil
}

// noMailer fails deliveries clearly when SMTP is not configured; messages stay queued.
type noMailer struct{}

func (noMailer) Send(context.Context, ports.Mail) error {
	return errors.New("SMTP is not configured (set CACTUSINV_SMTP_HOST)")
}

func serve(ctx context.Context, cfg config, log *slog.Logger) error {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if cfg.MigrateOnStart {
		if err := mariadb.Migrate(ctx, db, false); err != nil {
			return err
		}
		log.Info("migrations applied")
	}
	if err := os.MkdirAll(cfg.InvoicesDir, 0o750); err != nil {
		return fmt.Errorf("invoices dir: %w", err)
	}
	svc, closeSvc, err := newService(ctx, cfg, db, log)
	if err != nil {
		return err
	}
	defer closeSvc()
	if cfg.DefaultAdminUser != "" {
		created, err := svc.EnsureDefaultAdmin(ctx, cfg.DefaultAdminUser, cfg.DefaultAdminPassword)
		if err != nil {
			return err
		}
		if created {
			log.Warn("created the default administrator — it must choose a new password at the first sign-in", "login", cfg.DefaultAdminUser)
		}
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	store := mariadb.NewStore(db)
	arch := archive.Dir{Root: cfg.InvoicesDir}
	handler := httpapi.NewHandler(httpapi.Options{
		Service: svc, Clock: clock.System{}, Logger: log, CookieSecure: cfg.CookieSecure, Registry: reg, Web: web.FS(), Version: version, Build: build,
		MaxUploadBytes: int64(cfg.BackupMaxUploadMB) << 20,
		Ready:          map[string]httpapi.ReadyCheck{"database": store.Ping, "invoices": arch.Ping},
	})
	srv := &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}

	workCtx, stopWork := context.WithCancel(context.WithoutCancel(ctx))
	workersDone := make(chan struct{})
	if cfg.Workers {
		go func() {
			worker.New(svc, log, worker.Intervals{}, reg).Run(workCtx)
			close(workersDone)
		}()
	} else {
		close(workersDone)
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "version", version, "build", build, "workers", cfg.Workers)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		stopWork()
		<-workersDone
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	// The workers stop claiming new work at once, while the HTTP server drains: both well
	// inside the container's stop grace period (docker-compose stop_grace_period 30s, the
	// Kubernetes default 30s), so an e-mail being sent is recorded before the process ends.
	stopWork()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	select {
	case <-workersDone:
	case <-shutdownCtx.Done():
		log.Warn("workers did not stop in time")
	}
	return err
}

func adminCommand(ctx context.Context, cfg config, args []string) error {
	if len(args) != 2 || (args[0] != "create" && args[0] != "set-password") {
		return errors.New("usage: cactusinv admin create|set-password <login>  (password on stdin)")
	}
	pw, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && pw == "" {
		return errors.New("read password from stdin")
	}
	pw = strings.TrimRight(pw, "\r\n")
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	svc, closeSvc, err := newService(ctx, cfg, db, newLogger("error"))
	if err != nil {
		return err
	}
	defer closeSvc()
	ctx = app.WithActor(ctx, app.ActorOperator) // the audit trail names the shell, not "System"
	if args[0] == "create" {
		_, err = svc.CreateUser(ctx, args[1], pw)
		return err
	}
	return svc.SetPassword(ctx, args[1], pw)
}

func healthcheck() error {
	addr := os.Getenv("CACTUSINV_HTTP_ADDR")
	if addr == "" || strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
		if addr == "127.0.0.1" {
			addr += ":8080"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", resp.Status)
	}
	return nil
}
