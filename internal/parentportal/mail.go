package parentportal

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	PasswordFile string `json:"passwordFile"`
	From         string `json:"from"`
	TeamInbox    string `json:"teamInbox"`
}
type SendFunc func(context.Context, string, string, string) error

func NewMailer(cfg SMTPConfig) (SendFunc, error) {
	if cfg.Host == "" || (cfg.Port != 465 && cfg.Port != 587) || cfg.Username == "" {
		return nil, errors.New("SMTP host, username and TLS port 465 or 587 are required")
	}
	if _, err := emailAddress(cfg.From); err != nil {
		return nil, errors.New("valid sender address required")
	}
	if cfg.TeamInbox != "" {
		if _, err := emailAddress(cfg.TeamInbox); err != nil {
			return nil, errors.New("valid team OTP inbox required when configured")
		}
	}
	info, err := os.Stat(cfg.PasswordFile)
	if err != nil {
		return nil, errors.New("SMTP app password file is missing")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("SMTP app password file must be private (chmod 600)")
	}
	b, err := os.ReadFile(cfg.PasswordFile)
	if err != nil {
		return nil, err
	}
	password := strings.TrimSpace(string(b))
	if password == "" {
		return nil, errors.New("SMTP app password is empty")
	}
	return func(ctx context.Context, to, subject, body string) error {
		if _, err := emailAddress(to); err != nil {
			return err
		}
		if strings.ContainsAny(subject, "\r\n") {
			return errors.New("invalid email subject")
		}
		msg, id, err := emailMessage(cfg.From, to, subject, body)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		defer conn.Close()
		deadline, _ := ctx.Deadline()
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
		tlsConfig := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
		if cfg.Port == 465 {
			t := tls.Client(conn, tlsConfig)
			if err := t.HandshakeContext(ctx); err != nil {
				return err
			}
			conn = t
		}
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return err
		}
		defer client.Close()
		if cfg.Port == 587 {
			if err := client.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, password, cfg.Host)); err != nil {
			return err
		}
		if err := client.Mail(cfg.From); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			w.Close()
			return err
		}
		// DATA completion is the delivery acknowledgement; a later QUIT failure must not trigger duplicate invitations.
		if err := w.Close(); err != nil {
			return err
		}
		_ = client.Quit()
		log.Printf("Parent portal email accepted: to=%s message_id=%s", to, id)
		return nil
	}, nil
}

var emailTemplate = template.Must(template.New("email").Parse(`<!doctype html><html><body style="margin:0;padding:28px 12px;background:#f5f1ed;font-family:Arial,sans-serif;color:#18283e"><table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td align="center"><table role="presentation" width="560" cellspacing="0" cellpadding="0" style="width:100%;max-width:560px;background:#fffdfa;border-radius:12px;overflow:hidden"><tr><td style="background:#18283e;padding:27px 32px;border-bottom:4px solid #bc3877"><table role="presentation" width="100%"><tr><td style="color:#fff4e9;font-family:Georgia,serif;font-size:25px;letter-spacing:2px">ORCHIDS<div style="font:9px Arial,sans-serif;letter-spacing:2px;padding-top:5px">THE INTERNATIONAL SCHOOL</div></td><td align="right" style="font-size:9px;color:#c2cada">POWERED BY<br><img src="https://app.orionedge.ai/classroom/orion.png" width="28" height="28" alt="Orion logo" style="vertical-align:middle;margin-right:6px"><strong style="font-size:17px;line-height:1.8;letter-spacing:3px;color:white">ORION</strong></td></tr></table></td></tr><tr><td style="padding:32px"><div style="font-size:10px;letter-spacing:2px;color:#b03572;margin-bottom:17px">PARENT CONNECT</div><h1 style="font:normal 30px/1.2 Georgia,serif;margin:0 0 23px">{{.Title}}</h1>{{if .Code}}<div style="background:#f8edf2;border:1px solid #ead3df;border-radius:8px;text-align:center;padding:24px 12px;margin-bottom:24px"><span style="font-size:10px;color:#93647d;letter-spacing:2px">YOUR ONE-TIME CODE</span><div style="font-size:37px;letter-spacing:10px;font-weight:600;color:#a52f69;margin:12px 0">{{.Code}}</div><span style="font-size:11px;color:#7c7180">Valid for 5 minutes · Use once</span></div>{{end}}<div style="font-size:13px;line-height:1.8;color:#596375;overflow-wrap:anywhere">{{range .Lines}}{{.}}<br>{{end}}</div>{{if .URL}}<table role="presentation" cellspacing="0" cellpadding="0" style="margin-top:25px"><tr><td style="background:#b42e6e;border-radius:6px"><a href="{{.URL}}" style="display:inline-block;padding:15px 23px;color:white;text-decoration:none;font-size:13px;font-weight:bold">Open Parent Connect &rarr;</a></td></tr></table>{{end}}</td></tr><tr><td style="padding:22px 32px;border-top:1px solid #eee3e8;font-size:10px;line-height:1.8;color:#8e8591"><strong style="color:#66596b">Orchids International School × Orion</strong><br>A thoughtful connection between school and home.<br>Private classroom access</td></tr></table></td></tr></table></body></html>`))

func emailMessage(from, to, subject, body string) ([]byte, string, error) {
	id, err := secret()
	if err != nil {
		return nil, "", err
	}
	id += "@" + strings.SplitN(from, "@", 2)[1]
	data := struct {
		Title, Code, URL string
		Lines            []string
	}{Title: "Your classroom connection."}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.HasPrefix(line, "Verification code: ") {
			data.Code = strings.TrimPrefix(line, "Verification code: ")
			data.Title = "A little verification."
			continue
		}
		if strings.HasPrefix(line, "Portal: ") {
			data.URL = strings.TrimPrefix(line, "Portal: ")
		}
		data.Lines = append(data.Lines, line)
	}
	var html bytes.Buffer
	if err := emailTemplate.Execute(&html, data); err != nil {
		return nil, "", err
	}
	var parts bytes.Buffer
	multi := multipart.NewWriter(&parts)
	for _, part := range []struct{ kind, body string }{{"text/plain", body}, {"text/html", html.String()}} {
		w, err := multi.CreatePart(textproto.MIMEHeader{"Content-Type": {part.kind + "; charset=UTF-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
		if err != nil {
			return nil, "", err
		}
		q := quotedprintable.NewWriter(w)
		if _, err := q.Write([]byte(part.body)); err != nil {
			return nil, "", err
		}
		if err := q.Close(); err != nil {
			return nil, "", err
		}
	}
	if err := multi.Close(); err != nil {
		return nil, "", err
	}
	fromHeader := (&mail.Address{Name: "Orchids Parent Connect via Orion", Address: from}).String()
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n", fromHeader, to, mime.QEncoding.Encode("UTF-8", subject), time.Now().Format(time.RFC1123Z), id, multi.Boundary())
	return append([]byte(header), parts.Bytes()...), id, nil
}
