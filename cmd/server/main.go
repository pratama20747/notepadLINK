// Command server menjalankan HTTP server notepad-sharelink.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"notepad-sharelink/internal/config"
	"notepad-sharelink/internal/db/sqlc"
	"notepad-sharelink/internal/handler"
	"notepad-sharelink/internal/router"
	"notepad-sharelink/internal/service"
	"notepad-sharelink/internal/storage"
)

func main() {
	// Setup structured logger (JSON di production, text di development)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("gagal load config: %v", err)
	}

	setupLogger(cfg.IsProd)

	// Context untuk sinyal interrupt
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		slog.Error("gagal parse database url", "error", err)
		os.Exit(1)
	}
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		slog.Error("gagal konek ke database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("gagal ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("berhasil konek ke database")

	queries := sqlc.New(pool)

	noteService := service.NewNoteService(queries)

	// Inisialisasi R2 dan attachment service (opsional — kalau belum
	// dikonfigurasi, fitur attachment dinonaktifkan).
	var attachmentService *service.AttachmentService
	if cfg.R2Enabled() {
		r2Client, err := storage.NewR2Client(cfg)
		if err != nil {
			slog.Error("gagal init R2 client", "error", err)
			os.Exit(1)
		}
		attachmentService = service.NewAttachmentService(
			queries, r2Client, cfg.MaxImageAttachSize, cfg.MaxVideoAttachSize, cfg.MaxAttachmentsPerNote,
		)
	} else {
		slog.Warn("R2 belum dikonfigurasi — fitur attachment dinonaktifkan")
	}

	noteHandler := handler.NewNoteHandler(noteService)
	attachmentHandler := handler.NewAttachmentHandler(attachmentService, cfg.MaxVideoAttachSize)

	// Buat HTTP server dari router
	r := router.New(noteHandler, attachmentHandler, cfg)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Jalankan server di goroutine
	go func() {
		slog.Info("server berjalan", "port", cfg.Port, "prod", cfg.IsProd)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server gagal berjalan", "error", err)
			os.Exit(1)
		}
	}()

	// Tunggu sinyal interrupt
	<-ctx.Done()
	slog.Info("menerima sinyal shutdown, menghentikan server...")

	// Buat context dengan timeout untuk graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Shutdown server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server berhenti dengan graceful")
}

// setupLogger mengkonfigurasi slog sebagai default logger.
func setupLogger(isProd bool) {
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	if isProd {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}
