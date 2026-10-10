package parentportal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type message struct{ to, subject, body string }
type fixture struct {
	s        *Server
	messages []message
	failMail bool
	paths    []string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		if r.Header.Get("Authorization") != "" || strings.Contains(r.Header.Get("Cookie"), "parent") {
			t.Error("parent credentials leaked to relay")
		}
		if strings.HasSuffix(r.URL.Path, "redirect.m3u8") {
			w.Header().Set("Location", "/camera-a/index.m3u8?cookieCheck=1")
			w.WriteHeader(302)
			return
		}
		if strings.HasSuffix(r.URL.Path, "external.m3u8") {
			w.Header().Set("Location", "http://outside.invalid/video")
			w.WriteHeader(302)
			return
		}
		w.Header().Set("Set-Cookie", "upstream=private")
		fmt.Fprint(w, "#EXTM3U\nsegment.ts\n")
	}))
	t.Cleanup(upstream.Close)
	dir := t.TempDir()
	js := filepath.Join(dir, "hls.js")
	if err := os.WriteFile(js, []byte("// test player"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Origin: "https://parents.example.test", DBPath: filepath.Join(dir, "portal.db"), HLSOrigin: upstream.URL, HLSJS: js, SMTP: SMTPConfig{TeamInbox: "team@example.test"}}, func(_ context.Context, to, subject, body string) error {
		if f.failMail {
			return errors.New("mail unavailable")
		}
		f.messages = append(f.messages, message{to, subject, body})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f.s = s
	return f
}

func request(s *Server, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://parents.example.test"+path, strings.NewReader(body))
	r.Header.Set("Origin", s.cfg.Origin)
	r.RemoteAddr = "192.0.2.1:1234"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, wanted %d: %s", w.Code, status, w.Body.String())
	}
}
func cookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	t.Fatal("missing cookie ", name)
	return nil
}
func field(t *testing.T, body, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `: (.+)$`).FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatal("missing mail field ", name)
	}
	return m[1]
}
func invite(t *testing.T, f *fixture, email, camera string) string {
	t.Helper()
	if err := f.s.Invite(context.Background(), email, "Test classroom", camera); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.messages[len(f.messages)-1].body, "Password:") {
		t.Fatal("password in invitation")
	}
	return email
}
func begin(t *testing.T, f *fixture, email string) (*http.Cookie, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"email": email})
	w := request(f.s, "POST", "/api/login", string(b))
	requireStatus(t, w, 200)
	return cookie(t, w, f.s.cookieName("challenge")), field(t, f.messages[len(f.messages)-1].body, "Verification code")
}
func authenticate(t *testing.T, f *fixture, email string) *http.Cookie {
	t.Helper()
	c, code := begin(t, f, email)
	w := request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c)
	requireStatus(t, w, 200)
	return cookie(t, w, f.s.cookieName("session"))
}

func TestInvitationAndOTPRequired(t *testing.T) {
	f := setup(t)
	invite(t, f, "parent@example.test", "camera-a")
	if f.messages[0].to != "parent@example.test" || !strings.Contains(f.messages[0].body, f.s.cfg.Origin) {
		t.Fatal("invitation destination or link missing")
	}
	if err := f.s.Invite(context.Background(), "parent@example.test", "Test classroom", "camera-a"); err == nil {
		t.Fatal("invitation resent")
	}
	if len(f.messages) != 1 {
		t.Fatal("duplicate invitation")
	}
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", ""), 401)
	c, code := begin(t, f, "parent@example.test")
	if f.messages[1].to != "team@example.test" {
		t.Fatal("OTP did not go to team")
	}
	if strings.Contains(f.messages[1].body, "Password:") {
		t.Fatal("password resent with OTP")
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Domain != "" {
		t.Fatal("unsafe challenge cookie")
	}
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", "", c), 401)
	w := request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c)
	requireStatus(t, w, 200)
	sess := cookie(t, w, f.s.cookieName("session"))
	requireStatus(t, request(f.s, "GET", "/api/me", "", sess), 200)
	requireStatus(t, request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c), 401)
	var a Account
	f.s.db.First(&a)
	if a.PasswordHash != "" {
		t.Fatal("new account has a password")
	}
	var ss session
	f.s.db.First(&ss)
	if ss.Hash == sess.Value {
		t.Fatal("session stored in plaintext")
	}
}

func TestCameraIsolationAndLogout(t *testing.T) {
	f := setup(t)
	a := authenticate(t, f, invite(t, f, "a@example.test", "camera-a"))
	b := authenticate(t, f, invite(t, f, "b@example.test", "camera-b"))
	for _, c := range []*http.Cookie{a, b} {
		w := request(f.s, "GET", "/media/index.m3u8?camera=camera-other&url=http://outside.invalid", "", c)
		requireStatus(t, w, 200)
		if w.Header().Get("Set-Cookie") != "" {
			t.Fatal("relay cookies leaked")
		}
	}
	if fmt.Sprint(f.paths) != "[/camera-a/index.m3u8 /camera-b/index.m3u8]" {
		t.Fatalf("wrong camera paths: %v", f.paths)
	}
	for _, path := range []string{"/media/../camera-b/index.m3u8", "/media/%2e%2e/camera-b/index.m3u8", "/media/camera-b/index.m3u8", "/hls/camera-b/index.m3u8", "/api/cameras", "/media/index.m3u8/../../index.ts"} {
		requireStatus(t, request(f.s, "GET", path, "", a), 404)
	}
	w := request(f.s, "GET", "/media/redirect.m3u8", "", a)
	requireStatus(t, w, 302)
	if w.Header().Get("Location") != "/media/index.m3u8?cookieCheck=1" {
		t.Fatal("redirect escaped portal")
	}
	requireStatus(t, request(f.s, "GET", "/media/external.m3u8", "", a), 502)
	requireStatus(t, request(f.s, "POST", "/api/logout", "{}", a), 200)
	requireStatus(t, request(f.s, "GET", "/media/segment.ts", "", a), 401)
	requireStatus(t, request(f.s, "GET", "/media/segment.ts", "", b), 200)
	if err := f.s.Revoke("b@example.test"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(f.s, "GET", "/media/segment.ts", "", b), 401)
}

func TestOTPAttemptsExpiryAndConcurrentReplay(t *testing.T) {
	for _, mode := range []string{"attempts", "expiry", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			c, code := begin(t, f, invite(t, f, "parent@example.test", "camera-a"))
			switch mode {
			case "attempts":
				wrong := "000000"
				if code == wrong {
					wrong = "111111"
				}
				for i := 0; i < 5; i++ {
					requireStatus(t, request(f.s, "POST", "/api/verify", `{"code":"`+wrong+`"}`, c), 401)
				}
				requireStatus(t, request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c), 401)
			case "expiry":
				now := time.Now().Add(otpTTL + time.Second)
				f.s.now = func() time.Time { return now }
				requireStatus(t, request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c), 401)
			case "concurrent":
				var wg sync.WaitGroup
				status := make(chan int, 2)
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); status <- request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c).Code }()
				}
				wg.Wait()
				close(status)
				ok := 0
				for n := range status {
					if n == 200 {
						ok++
					} else if n != 401 {
						t.Fatalf("unexpected status %d", n)
					}
				}
				if ok != 1 {
					t.Fatalf("replay created %d sessions", ok)
				}
			}
		})
	}
}

func TestSessionExpiryAndNewLogin(t *testing.T) {
	f := setup(t)
	invite(t, f, "parent@example.test", "camera-a")
	first := authenticate(t, f, "parent@example.test")
	now := time.Now().Add(61 * time.Second)
	f.s.now = func() time.Time { return now }
	c, code := begin(t, f, "parent@example.test")
	requireStatus(t, request(f.s, "GET", "/api/me", "", c), 401)
	w := request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c)
	requireStatus(t, w, 200)
	second := cookie(t, w, f.s.cookieName("session"))
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", "", first), 401)
	now = now.Add(sessionTTL + time.Second)
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", "", second), 401)
}

func TestMailFailureAndRateLimits(t *testing.T) {
	f := setup(t)
	f.failMail = true
	if err := f.s.Invite(context.Background(), "failed@example.test", "Test", "camera-a"); err == nil {
		t.Fatal("mail failure ignored")
	}
	var count int64
	f.s.db.Model(&Account{}).Count(&count)
	if count != 0 {
		t.Fatal("failed invitation left account")
	}
	f.failMail = false
	invite(t, f, "parent@example.test", "camera-a")
	f.failMail = true
	b, _ := json.Marshal(map[string]string{"email": "parent@example.test"})
	w := request(f.s, "POST", "/api/login", string(b))
	requireStatus(t, w, 503)
	if len(w.Result().Cookies()) > 0 {
		t.Fatal("challenge cookie issued after mail failure")
	}
	f.s.db.Model(&challenge{}).Count(&count)
	if count != 0 {
		t.Fatal("challenge survived failed mail")
	}
	requireStatus(t, request(f.s, "POST", "/api/login", string(b)), 429)
	for i := 0; i < 8; i++ {
		requireStatus(t, request(f.s, "POST", "/api/login", `{"email":"unknown@example.test"}`), 401)
	}
	requireStatus(t, request(f.s, "POST", "/api/login", `{"email":"unknown@example.test"}`), 429)
}

func TestOriginAndRequestValidation(t *testing.T) {
	f := setup(t)
	for _, origin := range []string{"", "https://evil.example.test"} {
		r := httptest.NewRequest("POST", f.s.cfg.Origin+"/api/login", strings.NewReader(`{}`))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		f.s.ServeHTTP(w, r)
		requireStatus(t, w, 403)
	}
	for _, body := range []string{`{"email":"x","password":"x","camera":"camera-b"}`, `{} {}`, strings.Repeat("x", 5000)} {
		requireStatus(t, request(f.s, "POST", "/api/login", body), 400)
	}
	w := request(f.s, "GET", "/", "")
	requireStatus(t, w, 200)
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing frame protection")
	}
	if _, err := io.ReadAll(w.Result().Body); err != nil {
		t.Fatal(err)
	}
	if _, err := emailAddress("a@example.test\r\nBcc: attacker@example.test"); err == nil {
		t.Fatal("header injection accepted")
	}
	if err := f.s.Invite(context.Background(), "x@example.test", "Test", "../other"); err == nil {
		t.Fatal("unsafe camera accepted")
	}
}

func TestAccountCooldownSurvivesRestart(t *testing.T) {
	f := setup(t)
	invite(t, f, "parent@example.test", "camera-a")
	begin(t, f, "parent@example.test")
	restarted, err := New(f.s.cfg, f.s.send)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	body := `{"email":"parent@example.test"}`
	requireStatus(t, request(restarted, "POST", "/api/login", body), 429)
	restarted.now = func() time.Time { return time.Now().Add(61 * time.Second) }
	requireStatus(t, request(restarted, "POST", "/api/login", body), 200)
}

func TestOnlyInvitedActiveEmailsCanRequestOTP(t *testing.T) {
	f := setup(t)
	invite(t, f, "parent@example.test", "camera-a")
	requireStatus(t, request(f.s, "POST", "/api/login", `{"email":"unknown@example.test"}`), 401)
	if err := f.s.Revoke("parent@example.test"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(f.s, "POST", "/api/login", `{"email":"parent@example.test"}`), 401)
	if len(f.messages) != 1 {
		t.Fatal("sent OTP for unavailable account")
	}
}

func TestReissueAccess(t *testing.T) {
	f := setup(t)
	email := "parent@example.test"
	invite(t, f, email, "camera-a")
	sess := authenticate(t, f, email)
	f.s.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	pending, code := begin(t, f, email)
	f.failMail = true
	if err := f.s.Reissue(context.Background(), email, "K1 A", "camera-b"); err == nil {
		t.Fatal("failed send succeeded")
	}
	requireStatus(t, request(f.s, "GET", "/api/me", "", sess), 200)
	var before Account
	f.s.db.First(&before)
	if before.Camera != "camera-a" {
		t.Fatal("failed send changed existing access")
	}
	f.failMail = false
	if err := f.s.Reissue(context.Background(), email, "K1 A", "../other"); err == nil {
		t.Fatal("invalid camera accepted")
	}
	if err := f.s.Reissue(context.Background(), email, "K1 A", "camera-b"); err != nil {
		t.Fatal(err)
	}
	mail := f.messages[len(f.messages)-1]
	if strings.Contains(mail.body, "Password:") || !strings.Contains(mail.body, "Portal: https://parents.example.test/") || field(t, mail.body, "Classroom") != "K1 A" {
		t.Fatal("incomplete credential email")
	}
	requireStatus(t, request(f.s, "GET", "/api/me", "", sess), 401)
	requireStatus(t, request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, pending), 401)
	fresh := authenticate(t, f, email)
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", "", fresh), 200)
	if f.paths[len(f.paths)-1] != "/camera-b/index.m3u8" {
		t.Fatal("wrong reassigned camera")
	}
}

func TestRelaySessionStaysBoundToParent(t *testing.T) {
	f := setup(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cookieCheck") != "1" {
			t.Error("missing relay cookie check")
		}
		check, err := r.Cookie("cookieCheck")
		if err != nil || check.Value != "1" {
			t.Error("missing relay cookie")
		}
		if strings.HasSuffix(r.URL.Path, "index.m3u8") {
			http.SetCookie(w, &http.Cookie{Name: "hlsSession", Value: "relay-secret"})
		} else {
			c, err := r.Cookie("hlsSession")
			if err != nil || c.Value != "relay-secret" {
				http.Error(w, "missing session", 401)
				return
			}
		}
		fmt.Fprint(w, "#EXTM3U\nmain_stream.m3u8\n")
	}))
	defer upstream.Close()
	f.s.upstream, _ = url.Parse(upstream.URL)
	a := authenticate(t, f, invite(t, f, "a@example.test", "camera-a"))
	b := authenticate(t, f, invite(t, f, "b@example.test", "camera-b"))
	w := request(f.s, "GET", "/media/index.m3u8", "", a)
	requireStatus(t, w, 200)
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("relay session exposed")
	}
	requireStatus(t, request(f.s, "GET", "/media/main_stream.m3u8", "", a), 200)
	requireStatus(t, request(f.s, "GET", "/media/main_stream.m3u8", "", b), 401)
}

func TestLegacyAccountUsesEmailOTP(t *testing.T) {
	f := setup(t)
	invite(t, f, "parent@example.test", "camera-a")
	if err := f.s.db.Model(&Account{}).Where("email = ?", "parent@example.test").Updates(map[string]any{"password_hash": "legacy-unused-hash", "failed_logins": 5, "locked_until": time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	c, code := begin(t, f, "  PARENT@example.test  ")
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", "", c), 401)
	w := request(f.s, "POST", "/api/verify", `{"code":"`+code+`"}`, c)
	requireStatus(t, w, 200)
	requireStatus(t, request(f.s, "GET", "/media/index.m3u8", "", cookie(t, w, f.s.cookieName("session"))), 200)
}
