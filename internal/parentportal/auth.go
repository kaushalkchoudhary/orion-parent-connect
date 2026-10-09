package parentportal

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func passwordHash(v string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(v), bcrypt.DefaultCost)
	return string(b), err
}
func emailAddress(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid email address")
	}
	return v, nil
}

// Invite refuses to resend an already active account's credentials.
func (s *Server) Invite(ctx context.Context, email, className, camera string) error {
	email, err := emailAddress(email)
	if err != nil {
		return err
	}
	className = strings.TrimSpace(className)
	if className == "" || len(className) > 120 || strings.ContainsAny(className, "\r\n") || !cameraID.MatchString(camera) {
		return errors.New("valid class name and camera path are required")
	}
	password, err := secret()
	if err != nil {
		return err
	}
	ph, err := passwordHash(password)
	if err != nil {
		return err
	}
	a := Account{Email: email, ClassName: className, Camera: camera, PasswordHash: ph}
	// The unique address prevents concurrent invitation runs from issuing two accounts.
	if err := s.db.Create(&a).Error; err != nil {
		return errors.New("account already exists or could not be saved; no invitation sent")
	}
	body := s.invitationBody(email, password, className)
	if err := s.send(ctx, email, "Orchids Parent Connect | Your invitation", body); err != nil {
		// No active account or usable session is left behind after a failed invitation.
		if delErr := s.db.Delete(&a).Error; delErr != nil {
			return errors.New("invitation failed; inactive account requires operator cleanup")
		}
		return errors.New("invitation could not be delivered")
	}
	if err := s.db.Model(&a).Update("active", true).Error; err != nil {
		return errors.New("invitation sent but account activation failed; operator action required")
	}
	return nil
}

func (s *Server) invitationBody(email, password, className string) string {
	delivery := "sent to your email address"
	if s.cfg.SMTP.TeamInbox != "" {
		delivery = "sent to the school team at " + s.cfg.SMTP.TeamInbox + ". Contact the team to receive your code"
	}
	return fmt.Sprintf("Welcome to Orchids International School's Parent Connect, powered by Orion.\n\nPortal: %s\nEmail: %s\nPassword: %s\nClassroom: %s\n\nKeep these credentials private. Each sign-in requires a fresh verification code, %s. Codes expire after 5 minutes; viewing sessions last 30 minutes.\n", s.cfg.Origin+s.cfg.BasePath+"/", email, password, className, delivery)
}

// Reissue explicitly replaces credentials and the assigned classroom. A failed send rolls back access changes.
func (s *Server) Reissue(ctx context.Context, email, className, camera string) error {
	email, err := emailAddress(email)
	if err != nil {
		return err
	}
	className = strings.TrimSpace(className)
	if className == "" || len(className) > 120 || strings.ContainsAny(className, "\r\n") || !cameraID.MatchString(camera) {
		return errors.New("valid class name and camera path are required")
	}
	password, err := secret()
	if err != nil {
		return err
	}
	ph, err := passwordHash(password)
	if err != nil {
		return err
	}
	sent := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var a Account
		if err := tx.Where("email = ? AND active = ?", email, true).First(&a).Error; err != nil {
			return err
		}
		if err := tx.Model(&a).Updates(map[string]any{"password_hash": ph, "class_name": className, "camera": camera, "next_otp": time.Time{}, "failed_logins": 0, "locked_until": time.Time{}}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", a.ID).Delete(&session{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", a.ID).Delete(&challenge{}).Error; err != nil {
			return err
		}
		body := s.invitationBody(email, password, className) + "\nThis new password replaces your previous password.\n"
		if err := s.send(ctx, email, "Orchids Parent Connect | Your classroom access", body); err != nil {
			return errors.New("credentials could not be delivered; previous access preserved")
		}
		sent = true
		return nil
	})
	if sent && err != nil {
		return errors.New("email accepted but credential update failed; operator action required")
	}
	return err
}

func (s *Server) Revoke(email string) error {
	email, err := emailAddress(email)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var a Account
		if err := tx.Where("email = ?", email).First(&a).Error; err != nil {
			return err
		}
		if err := tx.Model(&a).Update("active", false).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", a.ID).Delete(&session{}).Error; err != nil {
			return err
		}
		return tx.Where("account_id = ?", a.ID).Delete(&challenge{}).Error
	})
}

// NotifyLink sends the updated portal address without reissuing credentials.
func (s *Server) NotifyLink(ctx context.Context, email string) error {
	email, err := emailAddress(email)
	if err != nil {
		return err
	}
	var a Account
	if err := s.db.Where("email = ? AND active = ?", email, true).First(&a).Error; err != nil {
		return err
	}
	destination := s.cfg.SMTP.TeamInbox
	if destination == "" {
		destination = a.Email
	}
	body := fmt.Sprintf("Your Orchids International School Parent Connect portal has a new look, powered by Orion.\n\nPortal: %s\nEmail: %s\nClassroom: %s\n\nUse the same password from your original invitation. We have not changed or resent it.\n\nEvery sign-in sends a fresh verification code to: %s\nCodes expire after 5 minutes. Check Spam or Junk if needed.\n", s.cfg.Origin+s.cfg.BasePath+"/", a.Email, a.ClassName, destination)
	return s.send(ctx, a.Email, "Orchids Parent Connect | Your updated portal link", body)
}

func (s *Server) allowLogin(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	// The service binds to loopback; only its local reverse proxy may assert the client IP.
	if net.ParseIP(ip).IsLoopback() {
		if v := net.ParseIP(r.Header.Get("X-Real-IP")); v != nil {
			ip = v.String()
		}
	}
	now := s.now()
	for key, v := range s.limits {
		if !v.Until.After(now) {
			delete(s.limits, key)
		}
	}
	v, exists := s.limits[ip]
	if !exists {
		if len(s.limits) >= 10000 {
			return false
		}
		v.Until = now.Add(15 * time.Minute)
	}
	v.Count++
	s.limits[ip] = v
	return v.Count <= 10
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.mu.Lock()
	if !s.allowLogin(r) {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Too many sign-in attempts. Try again in 15 minutes.")
		return
	}
	s.mu.Unlock()
	email, _ := emailAddress(in.Email)
	var a Account
	err := s.db.Where("email = ? AND active = ?", email, true).First(&a).Error
	ph := a.PasswordHash
	if err != nil {
		ph = s.dummyHash
	}
	passwordOK := bcrypt.CompareHashAndPassword([]byte(ph), []byte(in.Password)) == nil
	credentialsOK := false
	if err == nil {
		err = s.db.Transaction(func(tx *gorm.DB) error {
			var current Account
			if err := tx.Where("id = ? AND active = ?", a.ID, true).First(&current).Error; err != nil {
				return err
			}
			if current.LockedUntil.After(s.now()) {
				return nil
			}
			if !current.LockedUntil.IsZero() {
				current.FailedLogins = 0
				current.LockedUntil = time.Time{}
			}
			if passwordOK {
				current.FailedLogins = 0
				credentialsOK = true
			} else {
				current.FailedLogins++
				if current.FailedLogins >= 5 {
					current.LockedUntil = s.now().Add(15 * time.Minute)
				}
			}
			return tx.Model(&current).Updates(map[string]any{"failed_logins": current.FailedLogins, "locked_until": current.LockedUntil}).Error
		})
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	if !credentialsOK {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	token, err := secret()
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	code := fmt.Sprintf("%06d", n.Int64())
	now := s.now()
	c := challenge{Hash: hash(token), AccountID: a.ID, CodeHash: hash(token + ":" + code), ExpiresAt: now.Add(otpTTL)}
	cooldown := errors.New("cooldown")
	err = s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Account{}).Where("id = ? AND active = ? AND next_otp <= ? AND password_hash = ?", a.ID, true, now, a.PasswordHash).Update("next_otp", now.Add(time.Minute))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return cooldown
		}
		if err := tx.Where("account_id = ? OR expires_at <= ?", a.ID, now).Delete(&challenge{}).Error; err != nil {
			return err
		}
		if err := tx.Where("expires_at <= ?", now).Delete(&session{}).Error; err != nil {
			return err
		}
		return tx.Create(&c).Error
	})
	if errors.Is(err, cooldown) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "A code was requested recently. Wait one minute before signing in again.")
		return
	}
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	recipient := s.cfg.SMTP.TeamInbox
	instruction := "Share this code only with the intended account holder after checking the request."
	if recipient == "" {
		recipient = a.Email
		instruction = "Enter this code in the browser where you signed in. Do not share it with anyone."
	}
	body := fmt.Sprintf("Your verification code for Orchids International School's Parent Connect, powered by Orion.\n\nVerification code: %s\nAccount: %s\nClassroom: %s\nExpires: %s\n\n%s This code works once and expires after five minutes. If you did not request access, ignore this email.\n", code, a.Email, a.ClassName, c.ExpiresAt.UTC().Format(time.RFC3339), instruction)
	if err := s.send(r.Context(), recipient, "Orchids Parent Connect | Sign-in code", body); err != nil {
		_ = s.db.Where("hash = ?", c.Hash).Delete(&challenge{}).Error
		fail(w, 503, "The verification email could not be delivered. Try again in one minute.")
		return
	}
	s.cookie(w, "challenge", token, otpTTL)
	jsonResponse(w, 200, map[string]any{"expiresAt": c.ExpiresAt, "destination": recipient, "teamApproval": s.cfg.SMTP.TeamInbox != ""})
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	cookie, err := r.Cookie(s.cookieName("challenge"))
	if err != nil {
		fail(w, 401, "Start a new sign-in to request a code.")
		return
	}
	if len(in.Code) != 6 || strings.Trim(in.Code, "0123456789") != "" {
		fail(w, 400, "Enter the six-digit code.")
		return
	}
	token, err := secret()
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	now := s.now()
	ss := session{Hash: hash(token), ExpiresAt: now.Add(sessionTTL)}
	valid := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var c challenge
		if err := tx.Where("hash = ? AND expires_at > ? AND attempts < ?", hash(cookie.Value), now, 5).First(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if err := tx.Model(&c).Update("attempts", c.Attempts+1).Error; err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(c.CodeHash), []byte(hash(cookie.Value+":"+in.Code))) != 1 {
			return nil
		}
		var a Account
		if err := tx.Where("id = ? AND active = ?", c.AccountID, true).First(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		res := tx.Where("hash = ?", c.Hash).Delete(&challenge{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		ss.AccountID = a.ID
		if err := tx.Where("account_id = ?", a.ID).Delete(&session{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&ss).Error; err != nil {
			return err
		}
		valid = true
		return nil
	})
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	if !valid {
		fail(w, 401, "Code is incorrect, expired or already used. After five attempts, start a new sign-in.")
		return
	}
	s.cookie(w, "challenge", "", -time.Second)
	s.cookie(w, "session", token, sessionTTL)
	jsonResponse(w, 200, map[string]any{"expiresAt": ss.ExpiresAt})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName("session")); err == nil {
		if err := s.db.Where("hash = ?", hash(c.Value)).Delete(&session{}).Error; err != nil {
			fail(w, 503, "Could not sign out. Please try again.")
			return
		}
	}
	if c, err := r.Cookie(s.cookieName("challenge")); err == nil {
		if err := s.db.Where("hash = ?", hash(c.Value)).Delete(&challenge{}).Error; err != nil {
			fail(w, 503, "Could not cancel sign-in. Please try again.")
			return
		}
	}
	s.cookie(w, "session", "", -time.Second)
	s.cookie(w, "challenge", "", -time.Second)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
