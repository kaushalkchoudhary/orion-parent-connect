package parentportal

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// Run explicitly with PARENT_BROWSER_CHECK=1 and a locally installed Playwright module.
func TestBrowserFlow(t *testing.T) {
	if os.Getenv("PARENT_BROWSER_CHECK") != "1" {
		t.Skip("optional Chrome/FFmpeg integration check")
	}
	dir := t.TempDir()
	videoDir := filepath.Join(dir, "camera-a")
	if err := os.Mkdir(videoDir, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=15", "-t", "16", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "30", "-an", "-f", "hls", "-hls_time", "2", "-hls_list_size", "0", filepath.Join(videoDir, "index.m3u8"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic video: %v %s", err, b)
	}
	relay := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer relay.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	var mu sync.Mutex
	var outbox []message
	teamInbox := ""
	otpTo := "test.parent@example.test"
	if os.Getenv("PARENT_BROWSER_TEAM") == "1" {
		teamInbox = "team@example.test"
		otpTo = teamInbox
	}
	hlsJS := os.Getenv("PARENT_HLS_JS")
	if hlsJS == "" {
		hlsJS = filepath.Join("..", "..", "third_party", "hls.js", "hls.min.js")
	}
	s, err := New(Config{Origin: origin, BasePath: "/classroom", DBPath: filepath.Join(dir, "accounts.db"), HLSOrigin: relay.URL, HLSJS: hlsJS, SMTP: SMTPConfig{TeamInbox: teamInbox}}, func(_ context.Context, to, subject, body string) error {
		mu.Lock()
		defer mu.Unlock()
		outbox = append(outbox, message{to, subject, body})
		return nil
	})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Invite(context.Background(), "test.parent@example.test", "Test classroom · Bluebird", "camera-a"); err != nil {
		t.Fatal(err)
	}
	h := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This mailbox exists only inside this ephemeral loopback test, never in the service.
		if r.URL.Path == "/classroom/test-mailbox" {
			mu.Lock()
			defer mu.Unlock()
			last := outbox[len(outbox)-1]
			_ = json.NewEncoder(w).Encode(map[string]string{"to": last.to, "body": last.body})
			return
		}
		http.StripPrefix("/classroom", s).ServeHTTP(w, r)
	})}
	go h.Serve(listener)
	defer h.Close()
	cmd = exec.Command("node", "browser.check.mjs")
	cmd.Env = append(os.Environ(), "PARENT_TEST_ARTIFACTS="+dir, "PARENT_TEST_ORIGIN="+origin+"/classroom", "PARENT_TEST_OTP_TO="+otpTo)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, b)
	} else {
		t.Log(string(b))
	}
}
