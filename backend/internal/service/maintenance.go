package service

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
	"backend/internal/storage"
)

// MaintenanceService runs the periodic self-healing sweep. Without it stale
// pending events permanently block a user's generation gate, and old media /
// logs / temp objects accumulate unbounded. Token-pool and cookie-refresh
// upkeep is gone — channels are stateless upstream credentials.
type MaintenanceService struct {
	events          *repo.EventRepository
	users           *repo.UserRepository
	settings        *repo.SiteSettingRepository
	store           *storage.Client
	v1              *V1Service
	showcase        *repo.ShowcaseRepository
	orders          *repo.OrderRepository
	interval        time.Duration
	stalePending    time.Duration
	stalePendingVid time.Duration
	maxResumeAge    time.Duration
	mediaPruneEvery time.Duration
	lastMediaPrune  time.Time
}

func NewMaintenanceService(events *repo.EventRepository, users *repo.UserRepository, settings *repo.SiteSettingRepository, store *storage.Client, v1 *V1Service, showcase *repo.ShowcaseRepository, orders *repo.OrderRepository) *MaintenanceService {
	return &MaintenanceService{
		events:       events,
		users:        users,
		settings:     settings,
		store:        store,
		v1:           v1,
		showcase:     showcase,
		orders:       orders,
		interval:     60 * time.Second,
		stalePending: 600 * time.Second,
		// 视频比图片慢得多，单条执行预算就是 30 分钟（videoGenBudget），用图片的
		// 10 分钟去扫会在它出片前判死并退款。
		stalePendingVid: 30 * time.Minute,
		// 带上游 jobId 的逾期任务先尝试续等而不是判死；超过 3 小时总时限
		// （vivid 同款）才放弃走判死退款。
		maxResumeAge:    3 * time.Hour,
		mediaPruneEvery: 60 * time.Second,
	}
}

// Run drives the sweep every interval until ctx is cancelled. It runs one sweep
// immediately on startup so a freshly restarted process heals stuck state right
// away rather than after the first tick.
func (m *MaintenanceService) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	m.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *MaintenanceService) tick(ctx context.Context) {
	// 0. Auto-cancel unpaid recharge orders past their 30-min TTL.
	if m.orders != nil {
		if n, err := m.orders.ExpirePending(ctx, time.Now()); err != nil {
			log.Printf("maintenance: expire orders: %v", err)
		} else if n > 0 {
			log.Printf("maintenance: cancelled %d expired order(s)", n)
		}
	}

	// 1a. Pending events with an upstream job id that outlived their local
	//     goroutine (process restart) get resumed against the owning channel —
	//     the upstream job keeps running even while we were down.
	if m.v1 != nil {
		if resumable, err := m.events.ResumableStale(ctx, m.stalePending, m.stalePendingVid, m.maxResumeAge); err != nil {
			log.Printf("maintenance: resumable_stale: %v", err)
		} else {
			for _, e := range resumable {
				m.v1.ResumeStaleJob(ctx, e)
			}
			if len(resumable) > 0 {
				log.Printf("maintenance: resuming %d stale job(s) on their channels", len(resumable))
			}
		}
	}

	// 1b. Fail long-pending events so they stop blocking the per-user gate, and
	//    refund the credits debited up-front for each abandoned generation (the
	//    normal failure-refund path never ran for a process-restart orphan).
	if purged, err := m.events.PurgeStale(ctx, m.stalePending, m.stalePendingVid, m.maxResumeAge); err != nil {
		log.Printf("maintenance: purge_stale: %v", err)
	} else if len(purged) > 0 {
		refunded := 0
		cancelled := 0
		for _, e := range purged {
			// Stop the generation goroutine if it's still running, so it doesn't
			// keep grinding for minutes and surface a late "success" on this
			// just-abandoned event.
			if m.v1 != nil && m.v1.Inflight().Cancel(e.ID) {
				cancelled++
			}
			if e.UserID == "" || e.Cost <= 0 {
				continue
			}
			// Exactly-once: only refund if we win the claim (the in-flight request
			// may have already refunded itself on its own failure path).
			claimed, err := m.events.MarkRefunded(ctx, e.ID)
			if err != nil {
				log.Printf("maintenance: claim refund %s: %v", e.ID, err)
				continue
			}
			if !claimed {
				continue
			}
			if _, err := m.users.RefundCredits(ctx, e.UserID, e.Cost); err != nil {
				log.Printf("maintenance: refund abandoned event %s (user %s, %.0f): %v", e.ID, e.UserID, e.Cost, err)
			} else {
				refunded++
			}
		}
		log.Printf("maintenance: marked %d stale pending event(s) failed, refunded %d, cancelled %d in-flight", len(purged), refunded, cancelled)
	}

	// 2. Enforce the admin-configured log retention window.
	m.pruneLogs(ctx)

	// 3. Enforce the media retention window + sweep expired tmp/ (v1 API job
	//    results) and stale refs/ objects. Runs every 60s like the log prune;
	//    mediaPruneEvery still gates it in case the interval is ever shortened.
	if time.Since(m.lastMediaPrune) >= m.mediaPruneEvery {
		m.pruneMedia(ctx)
		m.lastMediaPrune = time.Now()
	}
}

func (m *MaintenanceService) pruneLogs(ctx context.Context) {
	days := m.retentionDays(ctx, "logs.retention_days")
	if days <= 0 {
		return
	}
	if _, err := m.events.PurgeOlderThan(ctx, time.Duration(days)*24*time.Hour); err != nil {
		log.Printf("maintenance: purge_older_than: %v", err)
	}
}

func (m *MaintenanceService) pruneMedia(ctx context.Context) {
	if m.store == nil || !m.store.Configured() {
		return
	}
	days := m.retentionDays(ctx, "media.retention_days")
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	// tmp/ holds /v1 job results: presigned for 30 min, pruned shortly after.
	tmpCutoff := time.Now().Add(-40 * time.Minute)
	// <uid>/refs/ holds uploaded reference material: live for the presign TTL.
	refCutoff := time.Now().Add(-2 * PresignTTL)
	objs, err := m.store.List(ctx, "")
	if err != nil {
		log.Printf("maintenance: list media: %v", err)
		return
	}
	// Files referenced by the homepage showcase are kept forever, no matter how
	// old — deleting them would break the public landing page.
	var pinned map[string]struct{}
	if m.showcase != nil {
		if pinned, err = m.showcase.PublicFileSet(ctx); err != nil {
			log.Printf("maintenance: showcase file set: %v", err)
			pinned = nil
		}
	}
	// The site logo is permanent too — pin it like a showcase image so the
	// retention sweep never deletes it. site.logo is "/images/<key>".
	if logo, _ := m.settings.GetValue(ctx, "site.logo"); strings.TrimSpace(logo) != "" {
		if pinned == nil {
			pinned = map[string]struct{}{}
		}
		pinned[strings.TrimPrefix(strings.TrimLeft(logo, "/"), "images/")] = struct{}{}
	}
	removed, skipped := 0, 0
	var clearedKeys []string
	for _, o := range objs {
		key := strings.TrimLeft(o.Key, "/")
		expired := false
		switch {
		case strings.HasPrefix(key, "tmp/"):
			expired = o.LastModified.Before(tmpCutoff)
		case strings.Contains(key, "/refs/"):
			expired = o.LastModified.Before(refCutoff)
		case days > 0:
			expired = o.LastModified.Before(cutoff)
		}
		if !expired {
			continue
		}
		if _, ok := pinned[key]; ok {
			skipped++
			continue
		}
		if err := m.store.Delete(ctx, o.Key); err != nil {
			log.Printf("maintenance: delete %s: %v", o.Key, err)
			continue
		}
		removed++
		// event_log.file stores the same key — blank those rows so the log views
		// don't dangle a 404 preview.
		clearedKeys = append(clearedKeys, o.Key)
	}
	if removed > 0 || skipped > 0 {
		log.Printf("maintenance: pruned %d expired media object(s), kept %d showcase-pinned", removed, skipped)
	}
	if len(clearedKeys) > 0 {
		if n, err := m.events.ClearFiles(ctx, clearedKeys); err != nil {
			log.Printf("maintenance: clear_files: %v", err)
		} else if n > 0 {
			log.Printf("maintenance: cleared file ref on %d log row(s)", n)
		}
	}
}

func (m *MaintenanceService) retentionDays(ctx context.Context, key string) int {
	raw, err := m.settings.GetValue(ctx, key)
	if err != nil {
		return 0
	}
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || days <= 0 {
		return 0
	}
	return days
}
