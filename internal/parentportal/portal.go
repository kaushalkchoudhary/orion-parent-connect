// Package parentportal serves an isolated, single-camera parent viewing portal.
package parentportal

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const sessionTTL = 30 * time.Minute
const otpTTL = 5 * time.Minute

type Config struct {
	Origin    string     `json:"origin"`
	BasePath  string     `json:"basePath"`
	Listen    string     `json:"listen"`
	DBPath    string     `json:"dbPath"`
	HLSOrigin string     `json:"hlsOrigin"`
	HLSJS     string     `json:"hlsJS"`
	SMTP      SMTPConfig `json:"smtp"`
}

type Account struct {
	ID           uint   `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string
	ClassName    string
	Camera       string
	Active       bool
	NextOTP      time.Time
	FailedLogins int
	LockedUntil  time.Time
}

type challenge struct {
	Hash      string `gorm:"primaryKey"`
	AccountID uint   `gorm:"uniqueIndex"`
	CodeHash  string
	ExpiresAt time.Time
	Attempts  int
}

type session struct {
	RelaySession string
	Hash         string `gorm:"primaryKey"`
	AccountID    uint   `gorm:"index"`
	ExpiresAt    time.Time
}

type limit struct {
	Count int
	Until time.Time
}

type Server struct {
	cfg       Config
	db        *gorm.DB
	send      SendFunc
	upstream  *url.URL
	secure    bool
	mu        sync.Mutex
	limits    map[string]limit
	now       func() time.Time
	dummyHash string
	transport *http.Transport
}

var cameraID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)
var mediaFile = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*\.(m3u8|ts|mp4|m4s)$`)

func New(cfg Config, send SendFunc) (*Server, error) {
	origin, err := url.Parse(cfg.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
		return nil, errors.New("origin must be an absolute origin without a trailing slash")
	}
	local := origin.Hostname() == "localhost" || net.ParseIP(origin.Hostname()).IsLoopback()
	if !regexp.MustCompile(`^(/[A-Za-z0-9_-]+)*$`).MatchString(cfg.BasePath) {
		return nil, errors.New("basePath must be empty or a path such as /classroom without a trailing slash")
	}
	if origin.Scheme != "https" && !(origin.Scheme == "http" && local) {
		return nil, errors.New("HTTPS is required outside localhost")
	}
	upstream, err := url.Parse(cfg.HLSOrigin)
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.User != nil || upstream.RawQuery != "" || upstream.Fragment != "" || (upstream.Path != "" && upstream.Path != "/") {
		return nil, errors.New("hlsOrigin must be a trusted relay origin without credentials or a path")
	}
	if cfg.DBPath == "" || cfg.HLSJS == "" {
		return nil, errors.New("dbPath and hlsJS are required")
	}
	if _, err := os.Stat(cfg.HLSJS); err != nil {
		return nil, fmt.Errorf("HLS player: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(cfg.DBPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(cfg.DBPath, 0600); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(cfg.DBPath+"?_busy_timeout=5000&_foreign_keys=on"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&Account{}, &challenge{}, &session{}); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if send == nil {
		send, err = NewMailer(cfg.SMTP)
		if err != nil {
			sqlDB.Close()
			return nil, err
		}
	}
	dummy, err := passwordHash("unused-placeholder-password")
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &Server{cfg: cfg, db: db, send: send, upstream: upstream, secure: origin.Scheme == "https", limits: map[string]limit{}, now: time.Now, dummyHash: dummy,
		transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 32, IdleConnTimeout: 30 * time.Second}}, nil
}

func (s *Server) Close() error {
	s.transport.CloseIdleConnections()
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func hash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }

func (s *Server) cookieName(kind string) string {
	if s.secure {
		return "__Host-parent_" + kind
	}
	return "parent_" + kind
}
func (s *Server) cookie(w http.ResponseWriter, kind, value string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	if ttl < 0 {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(kind), Value: value, Path: "/", MaxAge: maxAge, Expires: s.now().Add(ttl), Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "Invalid request.")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "Invalid request.")
		return false
	}
	return true
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; media-src 'self' blob:; worker-src 'self' blob:; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Method == http.MethodPost && r.Header.Get("Origin") != s.cfg.Origin {
		fail(w, 403, "Request origin rejected.")
		return
	}
	switch {
	case r.URL.Path == "/api/login" && r.Method == http.MethodPost:
		s.login(w, r)
	case r.URL.Path == "/api/verify" && r.Method == http.MethodPost:
		s.verify(w, r)
	case r.URL.Path == "/api/logout" && r.Method == http.MethodPost:
		s.logout(w, r)
	case r.URL.Path == "/api/me" && r.Method == http.MethodGet:
		a, ss, err := s.authorize(r)
		if err != nil {
			fail(w, 401, "Please sign in.")
			return
		}
		jsonResponse(w, 200, map[string]any{"className": a.ClassName, "email": a.Email, "expiresAt": ss.ExpiresAt})
	case strings.HasPrefix(r.URL.Path, "/media/") && r.Method == http.MethodGet:
		s.media(w, r)
	case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
		jsonResponse(w, 200, map[string]string{"status": "ok"})
	case r.Method == http.MethodGet:
		s.static(w, r)
	default:
		fail(w, 404, "Not found.")
	}
}

func (s *Server) authorize(r *http.Request) (Account, session, error) {
	var a Account
	var ss session
	c, err := r.Cookie(s.cookieName("session"))
	if err != nil {
		return a, ss, err
	}
	err = s.db.Where("hash = ? AND expires_at > ?", hash(c.Value), s.now()).First(&ss).Error
	if err != nil {
		return a, ss, err
	}
	err = s.db.Where("id = ? AND active = ?", ss.AccountID, true).First(&a).Error
	return a, ss, err
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	a, ss, err := s.authorize(r)
	if err != nil {
		fail(w, 401, "Please sign in.")
		return
	}
	file := strings.TrimPrefix(r.URL.Path, "/media/")
	if !mediaFile.MatchString(file) || strings.Contains(file, "..") || !cameraID.MatchString(a.Camera) {
		fail(w, 404, "Not found.")
		return
	}
	prefix := "/" + a.Camera + "/"
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(s.upstream)
			p.Out.URL.Path = prefix + file
			p.Out.URL.RawPath = ""
			// Every resource is mapped from the authenticated account, never a camera supplied by the client.
			q := url.Values{"cookieCheck": {"1"}}
			for _, k := range []string{"_HLS_msn", "_HLS_part", "_HLS_skip"} {
				if v := r.URL.Query().Get(k); v != "" {
					q.Set(k, v)
				}
			}
			p.Out.URL.RawQuery = q.Encode()
			p.Out.Header = make(http.Header)
			p.Out.AddCookie(&http.Cookie{Name: "cookieCheck", Value: "1"})
			if ss.RelaySession != "" {
				p.Out.AddCookie(&http.Cookie{Name: "hlsSession", Value: ss.RelaySession})
			}
		},
		Transport: s.transport,
		ModifyResponse: func(res *http.Response) error {
			// Keep the relay session server-side, bound to this authenticated parent session.
			for _, c := range res.Cookies() {
				if c.Name == "hlsSession" && c.Value != "" && len(c.Value) <= 256 {
					if err := s.db.Model(&session{}).Where("hash = ?", ss.Hash).Update("relay_session", c.Value).Error; err != nil {
						return err
					}
				}
			}
			res.Header.Del("Set-Cookie")
			res.Header.Del("Access-Control-Allow-Origin")
			res.Header.Set("Cache-Control", "no-store")
			if location := res.Header.Get("Location"); location != "" {
				u, err := url.Parse(location)
				if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, prefix) {
					return errors.New("unexpected relay redirect")
				}
				file := strings.TrimPrefix(u.Path, prefix)
				if !mediaFile.MatchString(file) || strings.Contains(file, "..") {
					return errors.New("unexpected relay redirect")
				}
				u.Path = s.cfg.BasePath + "/media/" + file
				res.Header.Set("Location", u.String())
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			fail(w, 502, "The classroom feed is temporarily unavailable.")
		},
	}
	proxy.ServeHTTP(w, r)
}
