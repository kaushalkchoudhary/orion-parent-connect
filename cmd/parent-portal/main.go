package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orion/parent-connect/internal/parentportal"
)

func main() {
	config := flag.String("config", "", "private JSON configuration path")
	action := flag.String("action", "serve", "serve, invite, reissue, notify, or revoke")
	email := flag.String("email", "", "recipient address")
	class := flag.String("class", "", "display name of the classroom")
	camera := flag.String("camera", "", "relay path for this account, e.g. orch-ptr-111-ch2-h264")
	flag.Parse()
	b, err := os.ReadFile(*config)
	if err != nil {
		log.Fatal("Read the private configuration: ", err)
	}
	var cfg parentportal.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Fatal("Invalid configuration: ", err)
	}
	s, err := parentportal.New(cfg, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	switch *action {
	case "invite":
		if err := s.Invite(context.Background(), *email, *class, *camera); err != nil {
			log.Fatal(err)
		}
		log.Print("Invitation sent; credentials are not logged.")
	case "reissue":
		if err := s.Reissue(context.Background(), *email, *class, *camera); err != nil {
			log.Fatal(err)
		}
		log.Print("Updated credentials and portal link sent; previous sessions revoked.")
	case "revoke":
		if err := s.Revoke(*email); err != nil {
			log.Fatal(err)
		}
		log.Print("Account access revoked.")
	case "notify":
		if err := s.NotifyLink(context.Background(), *email); err != nil {
			log.Fatal(err)
		}
		log.Print("Updated portal link sent; existing password unchanged.")
	case "serve":
		host, _, err := net.SplitHostPort(cfg.Listen)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			log.Fatal("Listen must be a loopback IP:port behind the dedicated HTTPS proxy")
		}
		h := &http.Server{Addr: cfg.Listen, Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-ctx.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = h.Shutdown(ctx)
		}()
		log.Print("Parent portal listening on ", cfg.Listen)
		if err := h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	default:
		log.Fatal("Unknown action")
	}
}
