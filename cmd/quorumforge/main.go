// Command quorumforge is the single-node HTTP backend for the QuorumForge
// root key signature ceremony sealing and isolation service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"quorumforge/internal/api"
	"quorumforge/internal/policy"
	"quorumforge/internal/service"
	"quorumforge/internal/store/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "quorumforge: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}

	st, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if cfg.Seed {
		if err := seed(st); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}

	svc := service.NewService(st)
	srv := api.New(svc, cfg.Addr)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("quorumforge listening on %s (db %s)", cfg.Addr, cfg.DBPath)
		if err := srv.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// seed installs a demo policy and participant directory so the service is
// usable out of the box.
func seed(st *sqlite.Store) error {
	ctx := context.Background()

	if err := st.UpsertPolicy(ctx, policy.Policy{
		Version:           "policy-v1",
		Threshold:         3,
		AllowedRoles:      []policy.Role{"officer", "auditor"},
		AllowedKeyVersion: "key-v1",
	}); err != nil {
		return err
	}

	participants := []policy.Participant{
		{PersonID: "alice", Credentials: []string{"alice-cred"}, Role: "officer", Revision: 1},
		{PersonID: "bob", Credentials: []string{"bob-cred"}, Role: "officer", Revision: 1},
		{PersonID: "carol", Credentials: []string{"carol-cred"}, Role: "officer", Revision: 1},
		{PersonID: "dave", Credentials: []string{"dave-cred"}, Role: "auditor", Revision: 1},
		{PersonID: "erin", Credentials: []string{"erin-cred"}, Role: "auditor", Revision: 1},
	}
	for _, p := range participants {
		if err := st.UpsertParticipant(ctx, p); err != nil {
			return err
		}
	}
	return nil
}
