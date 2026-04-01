package scheduler

import (
	"context"
	"log/slog"
	"time"
)

type JobFunc func(context.Context, time.Time) error

type AlignedScheduler struct {
	interval    time.Duration
	gracePeriod time.Duration
	runOnStart  bool
	job         JobFunc
	logger      *slog.Logger
}

func NewAligned(interval, gracePeriod time.Duration, runOnStart bool, logger *slog.Logger, job JobFunc) *AlignedScheduler {
	return &AlignedScheduler{interval: interval, gracePeriod: gracePeriod, runOnStart: runOnStart, job: job, logger: logger}
}

func (s *AlignedScheduler) Run(ctx context.Context) error {
	if s.runOnStart {
		s.runJob(ctx, time.Now().UTC())
	}

	for {
		nextRun := nextBoundary(time.Now().UTC(), s.interval).Add(s.gracePeriod)
		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		s.runJob(ctx, time.Now().UTC())
	}
}

func (s *AlignedScheduler) runJob(ctx context.Context, now time.Time) {
	s.logger.Info("run scheduled summary job", slog.Time("now", now))
	if err := s.job(ctx, now); err != nil && ctx.Err() == nil {
		s.logger.Error("scheduled summary job failed", slog.String("error", err.Error()))
	}
}

func nextBoundary(now time.Time, interval time.Duration) time.Time {
	boundary := now.Truncate(interval).Add(interval)
	if !boundary.After(now) {
		return boundary.Add(interval)
	}
	return boundary
}
