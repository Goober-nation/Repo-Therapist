package runtimeconfig

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DefaultWatchInterval is how often Watch checks app.yaml. Polling the content hash, not file
// events: editors replace files by rename and Docker bind mounts on macOS drop inotify events.
const DefaultWatchInterval = 2 * time.Second

// Watch reloads the store's file when its content changes or the process gets SIGHUP, until ctx
// ends. A failed reload is logged and the last good config stays in force.
func (s *Store) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-hup:
			log.Printf("config: SIGHUP, reloading %s", s.path)
		}
		if _, err := s.Reload(); err != nil {
			log.Printf("config: reload of %s failed, keeping the last good config: %v", s.path, err)
		}
	}
}

// Start is Init (with the sections Services lists for service) plus Watch in the background:
// the one call a service's main needs.
func Start(ctx context.Context, service string) (*Store, error) {
	sections, ok := Services[service]
	if !ok {
		return nil, fmt.Errorf("config: unknown service %q", service)
	}
	s, err := Init(Path(), sections[0], sections[1:]...)
	if err != nil {
		return nil, err
	}
	go s.Watch(ctx, DefaultWatchInterval)
	return s, nil
}
