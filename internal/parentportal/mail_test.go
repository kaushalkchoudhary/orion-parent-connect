package parentportal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestBrandedEmailAlternatives(t *testing.T) {
	body := "Verification code: 123456\nAccount: test@example.test\nClassroom: <script>alert(1)</script>\n"
	raw, id, err := emailMessage("sender@example.test", "parent@example.test", "Orchids Parent Connect | Verification code", body)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Message-ID") != "<"+id+">" {
		t.Fatal("missing message ID")
	}
	if strings.Contains(msg.Header.Get("Subject"), "123456") {
		t.Fatal("code leaked in subject")
	}
	kind, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/alternative" {
		t.Fatal("missing alternatives")
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	plain, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(plain)
	if !strings.Contains(string(b), "Verification code: 123456") {
		t.Fatal("plain code missing")
	}
	html, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(html)
	for _, want := range []string{"ORCHIDS", "ORION", ">123456</code>", "Your verification code.", "&lt;script&gt;"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("missing HTML content ", want)
		}
	}
	if strings.Contains(string(b), "<script>") {
		t.Fatal("HTML injection")
	}
	if _, err := parts.NextPart(); err != io.EOF {
		t.Fatal("unexpected MIME parts")
	}
}

func TestViewerOTPAndUpdatedLink(t *testing.T) {
	f := setup(t)
	f.s.cfg.SMTP.TeamInbox = ""
	f.s.cfg.BasePath = "/classroom"
	password := invite(t, f, "parent@example.test", "camera-a")
	if !strings.Contains(f.messages[0].body, "https://parents.example.test/classroom/") {
		t.Fatal("invitation uses wrong path")
	}
	b, _ := json.Marshal(map[string]string{"email": "parent@example.test", "password": password})
	w := request(f.s, "POST", "/api/login", string(b))
	requireStatus(t, w, 200)
	var result struct {
		Destination  string
		TeamApproval bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Destination != "parent@example.test" || result.TeamApproval {
		t.Fatal("wrong UI destination")
	}
	otp := f.messages[1]
	if otp.subject != "Orchids Parent Connect | Verification code" || !strings.HasPrefix(otp.body, "Verification code: ") || strings.Contains(otp.body, "Expires:") {
		t.Fatal("OTP email should lead with one clearly labelled code, without a numeric timestamp")
	}
	if f.messages[1].to != "parent@example.test" {
		t.Fatal("OTP sent to wrong inbox")
	}
	if err := f.s.NotifyLink(context.Background(), "parent@example.test"); err != nil {
		t.Fatal(err)
	}
	last := f.messages[len(f.messages)-1]
	if strings.Contains(last.body, password) || !strings.Contains(last.body, "https://parents.example.test/classroom/") {
		t.Fatal("updated link resends password or uses wrong URL")
	}
}

func TestSubpathMediaRedirect(t *testing.T) {
	f := setup(t)
	f.s.cfg.BasePath = "/classroom"
	a := authenticate(t, f, "parent@example.test", invite(t, f, "parent@example.test", "camera-a"))
	w := request(f.s, "GET", "/media/redirect.m3u8", "", a)
	requireStatus(t, w, 302)
	if w.Header().Get("Location") != "/classroom/media/index.m3u8?cookieCheck=1" {
		t.Fatal("redirect escaped classroom path")
	}
}
