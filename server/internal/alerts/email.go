package alerts

import (
	"crypto/tls"
	"fmt"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// sendCriticalEmail notifies the configured recipient about a newly-opened
// critical alert. It is a no-op unless SMTP_ENABLED and the target address are
// configured. Sent asynchronously so evaluation never blocks on the mail server.
func (e *Engine) sendCriticalEmail(a model.AlertDTO, lang string) {
	if !e.cfg.SMTPEnabled || e.cfg.AlertEmailTo == "" || e.cfg.SMTPHost == "" {
		return
	}
	go func() {
		if err := e.deliverEmail(a, lang); err != nil {
			log.Printf("alerts: e-mail send failed: %v", err)
		}
	}()
}

// buildEmail renders the subject and body of a critical-alert e-mail in lang
// (the instance default language). The alert text is re-rendered from the
// structured params rather than taken from the stored fallback message.
func buildEmail(a model.AlertDTO, lang string) (subject, body string) {
	lang = NormalizeLang(lang)
	t := emailTexts[lang]
	text := RenderMessage(lang, a.Rule, a.Params)
	subject = oneLine(fmt.Sprintf(t.subject, text))
	body = fmt.Sprintf("%s\r\n\r\n%s: %s (%s)\r\n%s: %s (%s)\r\n%s: %s\r\n%s: %s\r\n",
		t.intro,
		t.machine, oneLine(a.Hostname), oneLine(a.MachineID),
		t.rule, RuleName(lang, a.Rule), a.Rule,
		t.message, text,
		t.time, a.OpenedAt.In(time.Local).Format("2006-01-02 15:04:05"))
	return subject, body
}

func (e *Engine) deliverEmail(a model.AlertDTO, lang string) error {
	from := e.cfg.SMTPFrom
	if from == "" {
		from = e.cfg.SMTPUser
	}
	to := e.cfg.AlertEmailTo

	subject, body := buildEmail(a, lang)

	msg := strings.Builder{}
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	// RFC 2047 encoded-word: the subject is non-ASCII in both languages (hostnames,
	// Turkish letters) and must never carry a raw line break into the headers.
	msg.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := net.JoinHostPort(e.cfg.SMTPHost, fmt.Sprintf("%d", e.cfg.SMTPPort))
	var auth smtp.Auth
	if e.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", e.cfg.SMTPUser, e.cfg.SMTPPass, e.cfg.SMTPHost)
	}

	// Port 465 implies implicit TLS; other ports use STARTTLS via smtp.SendMail.
	if e.cfg.SMTPPort == 465 {
		return sendImplicitTLS(addr, e.cfg.SMTPHost, auth, from, []string{to}, msg.String())
	}
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg.String()))
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from string, to []string, msg string) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
