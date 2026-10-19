package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"backend/internal/config"
	"backend/internal/http/handler"
	"backend/internal/http/router"
	"backend/internal/model"
	"backend/internal/repo"
	"backend/internal/service"
	"backend/internal/storage"
	"backend/internal/upstream"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type App struct {
	Config            *config.Config
	DB                *gorm.DB
	Redis             *redis.Client
	Engine            *gin.Engine
	maintenanceCancel context.CancelFunc
}

func NewApp(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Ensure the media root (generated outputs + uploaded reference images)
	// exists from the first request — don't rely on lazy per-file MkdirAll.
	if err := os.MkdirAll(cfg.GeneratedRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create generated root %s: %w", cfg.GeneratedRoot, err)
	}

	// TranslateError: 把驱动层错误(如 Postgres 23505 唯一冲突)翻译成
	// gorm.ErrDuplicatedKey,否则 errors.Is(err, gorm.ErrDuplicatedKey) 兜底命不中,
	// 重复导入会直接抛原始错误 → 400,而不是按预期 Update 已有行。
	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("sql db: %w", err)
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := db.WithContext(ctx).AutoMigrate(model.AutoMigrateModels()...); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	// Hard backstop for "one marketing code per user per batch": a partial unique
	// index. Concurrent double-redeems that slip past the in-tx count check still
	// fail here. AutoMigrate can't express partial indexes, so do it raw.
	if err := db.WithContext(ctx).Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_cdk_marketing_batch_user ` +
		`ON cdk_codes (batch_id, redeemed_by) WHERE type = 'marketing' AND redeemed_by IS NOT NULL`).Error; err != nil {
		return nil, fmt.Errorf("cdk marketing index: %w", err)
	}
	// 配价即上架（vivid 同款）：models 不再有启停开关，删遗留列。
	if err := db.WithContext(ctx).Exec(`ALTER TABLE model_configs DROP COLUMN IF EXISTS enabled`).Error; err != nil {
		return nil, fmt.Errorf("drop model_configs.enabled: %w", err)
	}
	if err := seedDefaults(ctx, db); err != nil {
		return nil, fmt.Errorf("seed defaults: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	userRepo := repo.NewUserRepository(db)
	showcaseRepo := repo.NewShowcaseRepository(db)
	siteRepo := repo.NewSiteSettingRepository(db, rdb)
	modelRepo := repo.NewModelRepository(db)
	eventRepo := repo.NewEventRepository(db)
	cdkRepo := repo.NewCDKRepository(db)
	apiKeyRepo := repo.NewAPIKeyRepository(db)
	channelRepo := repo.NewChannelRepository(db)
	cgroupRepo := repo.NewConcurrencyGroupRepository(db)
	// Seed the "默认并发" group (cap 10) and bind any ungrouped users to it.
	if err := cgroupRepo.EnsureDefault(ctx); err != nil {
		log.Printf("ensure default concurrency group: %v", err)
	}
	concSvc := service.NewConcurrencyService(rdb)
	cgroupSvc := service.NewConcurrencyGroupService(cgroupRepo, concSvc)
	announcementSvc := service.NewAnnouncementService(siteRepo, userRepo)
	orderRepo := repo.NewOrderRepository(db)
	paymentSvc := service.NewPaymentService(orderRepo, userRepo, siteRepo)
	sessionSvc := service.NewSessionService(rdb, cfg.SessionTTL, cfg.SessionSlideAfter)
	emailCodeSvc := service.NewEmailCodeService(rdb)
	smtpSvc := service.NewSMTPService()
	rateLimitSvc := service.NewRateLimitService(rdb)
	rustfsClient := storage.New(cfg.RustFSEndpoint, cfg.RustFSBucket, cfg.RustFSAccessKey, cfg.RustFSSecretKey)
	authSvc := service.NewAuthService(userRepo, siteRepo, sessionSvc, emailCodeSvc, smtpSvc, cgroupRepo)
	appSettingsSvc := service.NewAppSettingsService(siteRepo, eventRepo, smtpSvc, rustfsClient)
	imageAccessSvc := service.NewImageAccessService(cfg.GeneratedRoot, showcaseRepo, authSvc)
	upClient := upstream.NewClient()
	channelSecret := cfg.ChannelSecret
	if channelSecret == "" {
		// Stable fallback so a restarted process can still decrypt existing
		// rows when CHANNEL_SECRET isn't set.
		channelSecret = cfg.PostgresDSN
	}
	channelSvc := service.NewChannelService(channelRepo, modelRepo, eventRepo, concSvc, upClient, channelSecret)
	// Sweep provider-era model rows: anything not bound to a channel can never
	// be routed under the channel scheduler.
	if n, err := channelSvc.PurgeOrphanModels(ctx); err != nil {
		log.Printf("purge orphan models: %v", err)
	} else if n > 0 {
		log.Printf("purged %d unbound model(s) left over from the provider era", n)
	}
	v1Svc := service.NewV1Service(cfg, modelRepo, userRepo, eventRepo, channelSvc, siteRepo, cgroupRepo, concSvc, rustfsClient)
	siteSvc := service.NewSiteService(siteRepo, cfg.AppTitle)
	showcaseSvc := service.NewShowcaseService(showcaseRepo)
	adminReadSvc := service.NewAdminReadService(cfg, userRepo, modelRepo, eventRepo, siteRepo, channelRepo, cdkRepo, rustfsClient, showcaseRepo)
	adminWriteSvc := service.NewAdminWriteService(userRepo, showcaseRepo, modelRepo, eventRepo, apiKeyRepo, channelRepo, orderRepo)
	cdkSvc := service.NewCDKService(cdkRepo, userRepo, siteRepo, orderRepo)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo)
	bannedWordRepo := repo.NewBannedWordRepository(db)
	v1Svc.SetBannedWords(bannedWordRepo)
	userGenSvc := service.NewUserGenerationService(v1Svc, eventRepo, userRepo, modelRepo)
	canvasRepo := repo.NewCanvasRepository(db)
	canvasSvc := service.NewCanvasService(canvasRepo)

	engine := router.New(cfg, authSvc, router.Handlers{
		Health:       handler.NewHealthHandler(),
		Images:       handler.NewImageHandler(cfg, imageAccessSvc, rustfsClient),
		V1:           handler.NewV1Handler(v1Svc),
		Site:         handler.NewSiteHandler(siteSvc),
		Showcase:     handler.NewShowcaseHandler(showcaseSvc),
		Auth:         handler.NewAuthHandler(cfg, authSvc, rateLimitSvc),
		SiteSettings: handler.NewSiteSettingsHandler(siteSvc),
		AppSettings:  handler.NewAppSettingsHandler(appSettingsSvc),
		AdminRead:    handler.NewAdminReadHandler(adminReadSvc),
		AdminWrite:   handler.NewAdminWriteHandler(adminWriteSvc),
		CDK:          handler.NewCDKHandler(cdkSvc),
		UserTools:    handler.NewUserToolsHandler(apiKeySvc, cdkSvc),
		UserGen:      handler.NewUserGenerationHandler(userGenSvc, adminReadSvc),
		ChannelAdmin: handler.NewChannelAdminHandler(channelSvc),
		ConcGroups:   handler.NewConcurrencyGroupHandler(cgroupSvc),
		Announcement: handler.NewAnnouncementHandler(announcementSvc),
		Payment:      handler.NewPaymentHandler(paymentSvc),
		BannedWords:  handler.NewBannedWordsHandler(bannedWordRepo),
		Canvas:       handler.NewCanvasHandler(canvasSvc),
	})

	// Background self-healing sweep (order expiry, stale-pending cleanup+refund,
	// log/media retention).
	maintenanceSvc := service.NewMaintenanceService(eventRepo, userRepo, siteRepo, rustfsClient, v1Svc, showcaseRepo, orderRepo)
	loopCtx, loopCancel := context.WithCancel(context.Background())
	go maintenanceSvc.Run(loopCtx)

	return &App{
		Config:            cfg,
		DB:                db,
		Redis:             rdb,
		Engine:            engine,
		maintenanceCancel: loopCancel,
	}, nil
}

func (a *App) Close() error {
	if a.maintenanceCancel != nil {
		a.maintenanceCancel()
	}
	if a.Redis != nil {
		if err := a.Redis.Close(); err != nil {
			return err
		}
	}
	if a.DB != nil {
		sqlDB, err := a.DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}
