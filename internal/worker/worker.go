// Package worker runs the scheduled jobs: due-date reminders, recurring
// cards, stale session links, clean-up and backups.
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"kowhai/internal/service"
)

// Job is one scheduled task.
type Job struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
	// Wait skips the run at start-up, so restarts do not repeat the job.
	Wait bool
}

// Jobs returns the standard job list. backup may be nil.
func Jobs(s *service.Service, backup func(context.Context) error) []Job {
	jobs := []Job{
		{Name: "due-reminders", Every: time.Minute, Run: func(ctx context.Context) error { _, err := s.SendDueReminders(ctx); return err }},
		{Name: "recurrences", Every: time.Minute, Run: func(ctx context.Context) error { _, err := s.RunDueRecurrences(ctx); return err }},
		{Name: "stale-links", Every: time.Minute, Run: func(ctx context.Context) error { _, err := s.MarkStaleLinks(ctx); return err }},
		{Name: "blob-cleanup", Every: time.Minute, Run: func(ctx context.Context) error { _, err := s.CleanupBlobs(ctx); return err }},
		{Name: "sessions", Every: time.Hour, Run: s.CleanupSessions},
		{Name: "rebalance", Every: 10 * time.Minute, Run: s.Rebalance},
	}
	if backup != nil {
		jobs = append(jobs, Job{Name: "backup", Every: 24 * time.Hour, Run: backup, Wait: true})
	}
	return jobs
}

// Run starts every job and blocks until ctx is done.
func Run(ctx context.Context, log *slog.Logger, jobs []Job) {
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			t := time.NewTicker(j.Every)
			defer t.Stop()
			if j.Wait {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
			for {
				start := time.Now()
				if err := j.Run(ctx); err != nil && ctx.Err() == nil {
					log.Error("job failed", "job", j.Name, "err", err)
				} else if d := time.Since(start); d > 5*time.Second {
					log.Info("job slow", "job", j.Name, "took", d)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}(j)
	}
	wg.Wait()
}
