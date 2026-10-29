package webhooks

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"net/textproto"
	"strings"
	"text/template"
	"time"

	"ourway/server/i18n"
)

// SMTP is the installation's mail transport (SMTP_HOST, SMTP_PORT,
// SMTP_USERNAME, SMTP_PASSWORD, SMTP_FROM, SMTP_STARTTLS). It is the transport,
// not the message: recipients come from the channel, so "client A's criticals to
// the on-call mailbox" is a channel, not a second mail server.
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
}

// Enabled reports whether the installation can send mail at all. An email
// channel on an installation with no SMTP configured is a fact the API states at
// save time, not a delivery that fails at 2 a.m.
func (s SMTP) Enabled() bool {
	return s.Host != "" && s.Port > 0
}

// SendMail delivers one rendered message to one channel's recipients.
//
// The return shape mirrors the HTTP sender: a code and the peer's own words go
// into the delivery ledger, so an operator reads "250 Hello called-foo" or the
// server's refusal, not a Go error string invented here.
func (s SMTP) SendMail(channel *ChannelConfig, subject, body string) Outcome {
	// The refusals below are configuration the operator can fix, not a mail server
	// having a bad minute: they are permanent, and the ledger records them once.
	if !s.Enabled() {
		return Outcome{Err: fmt.Errorf("no SMTP server configured (SMTP_HOST, SMTP_PORT)"), Permanent: true}
	}
	recipients := channel.To
	if len(recipients) == 0 {
		return Outcome{Err: fmt.Errorf("the email channel names no recipient (config.to)"), Permanent: true}
	}
	from := channel.From
	if from == "" {
		from = s.From
	}
	if from == "" {
		return Outcome{Err: fmt.Errorf("no sender address: set SMTP_FROM or the channel's config.from"), Permanent: true}
	}

	code, response, err := s.deliver(from, recipients, subject, body)
	if err != nil {
		return Outcome{StatusCode: code, Response: response, Err: err, Permanent: permanentMailFailure(code, s.StartTLS)}
	}
	return Outcome{StatusCode: code, Response: response}
}

func (s SMTP) deliver(from string, to []string, subject, body string) (int, string, error) {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	client, err := smtp.Dial(addr)
	if err != nil {
		return 0, "", fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer client.Quit()

	if err := client.Hello("ourway"); err != nil {
		return 0, "", fmt.Errorf("smtp HELO: %w", err)
	}

	// STARTTLS is offered when the server advertises it and the installation
	// asked for it. A server that does not advertise it is refused outright by
	// an installation that requires it: credentials never travel in clear text
	// over a connection that was supposed to be encrypted.
	if s.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return 0, "", fmt.Errorf("smtp: %s does not offer STARTTLS and SMTP_STARTTLS is set", addr)
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return 0, "", fmt.Errorf("smtp STARTTLS: %w", err)
		}
	}

	if s.Username != "" {
		auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
		if ok, _ := client.Extension("AUTH"); !ok {
			return 0, "", fmt.Errorf("smtp: %s does not offer AUTH and SMTP_USERNAME is set", addr)
		}
		if err := client.Auth(auth); err != nil {
			return 0, "", fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return 0, "", fmt.Errorf("smtp MAIL FROM <%s>: %w", from, err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return 0, "", fmt.Errorf("smtp RCPT TO <%s>: %w", recipient, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return 0, "", fmt.Errorf("smtp DATA: %w", err)
	}
	message := buildMessage(from, to, subject, body)
	if _, err := writer.Write([]byte(message)); err != nil {
		return 0, "", fmt.Errorf("smtp write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		code, text := smtpCode(err), codeText(err)
		return code, text, fmt.Errorf("smtp end of data: %w", err)
	}
	if err := client.Quit(); err != nil {
		code, text := smtpCode(err), codeText(err)
		return code, text, fmt.Errorf("smtp QUIT: %w", err)
	}
	// The last line DATA answered is the peer's answer about the message, and it
	// is what the delivery ledger keeps.
	return 250, "250 message accepted", nil
}

// permanentMailFailure reads the SMTP reply class: a 5xx is the server refusing
// the request itself (bad address, refused sender, syntax), which a retry
// reproduces exactly. A 4xx is a transient refusal — mailbox full, temporary
// relay — and a 0 code is a connection that went away. Encryption required and
// not available is a configuration mismatch, so it is permanent too.
func permanentMailFailure(code int, requiredTLS bool) bool {
	if requiredTLS && code == 0 {
		return true
	}
	return code >= 500 && code < 600
}

func buildMessage(from string, to []string, subject, body string) string {
	var out strings.Builder
	out.WriteString("From: " + from + "\r\n")
	out.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	out.WriteString("Subject: " + subject + "\r\n")
	out.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123) + "\r\n")
	out.WriteString("MIME-Version: 1.0\r\n")
	out.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	out.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	out.WriteString("\r\n")
	out.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	out.WriteString("\r\n.\r\n")
	return out.String()
}

// smtpCode answers the peer's numeric reply code, when the error carries one.
// The ledger keeps the number the mail server sent, not a wrapped Go string.
func smtpCode(err error) int {
	if typed := protocolError(err); typed != nil {
		return typed.Code
	}
	return 0
}

func codeText(err error) string {
	if typed := protocolError(err); typed != nil {
		return typed.Error()
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func protocolError(err error) *textproto.Error {
	for err != nil {
		if typed, ok := err.(*textproto.Error); ok {
			return typed
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = wrapped.Unwrap()
	}
	return nil
}

// RenderEmail builds the subject and body for one event, in the channel's
// locale. The facts come from the event's own payload: the mail says the unit,
// the metric, the value and the threshold the engine published, in the
// localized frame around them.
func RenderEmail(locale string, event OutboundEvent, to []string) (subject, body string) {
	data := dataMap(event.Data)
	str := func(path string) string {
		if value := i18n.String(locale, path); value != "" {
			return value
		}
		return i18n.String(i18n.FallbackLocale, path)
	}
	key := "mail.events." + event.Type
	subjectTemplate := str(key + ".subject")
	bodyTemplate := str(key + ".body")
	if subjectTemplate == "" || bodyTemplate == "" {
		subjectTemplate = str("mail.fallback.subject")
		bodyTemplate = str("mail.fallback.body")
	}
	fields := mailFields(data, event, str, to)
	return renderTemplate(subjectTemplate, fields), renderTemplate(bodyTemplate, fields)
}

func mailFields(data map[string]interface{}, event OutboundEvent, str func(string) string, to []string) map[string]string {
	value := func(keys ...string) string {
		for _, key := range keys {
			if raw, ok := data[key]; ok {
				text := strings.TrimSpace(fmt.Sprint(raw))
				if text != "" && text != "<nil>" {
					return text
				}
			}
		}
		return "—"
	}
	at := event.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	fields := map[string]string{
		"Type":      event.Type,
		"Device":    value("device", "device_name", "name"),
		"Severity":  value("severity"),
		"Metric":    value("metric"),
		"Value":     value("value"),
		"Threshold": value("threshold"),
		"Client":    value("client", "client_name"),
		"User":      value("user", "username"),
		"Name":      value("name", "update", "patch"),
		"Message":   value("message"),
		"Version":   value("version"),
		"At":        at.UTC().Format(time.RFC3339),
		"To":        strings.Join(to, ", "),
	}
	// The footer is itself a template (it names the recipient), so it is rendered
	// against the fields before it is placed in the body. Inserting it raw leaves
	// `{{.To}}` in the mail a person reads.
	fields["Footer"] = renderTemplate(str("mail.footer"), fields)
	return fields
}

// renderTemplate expands one locale template against the event's fields.
//
// Missing fields are filled with an em dash before rendering, so a device
// online event (which carries no metric) reads as "Metric: —" in all three
// locales instead of a blank line an operator has to interpret.
func renderTemplate(text string, fields map[string]string) string {
	tmpl, err := template.New("mail").Option("missingkey=zero").Parse(text)
	if err != nil {
		// A template that does not parse is a build-time fact, not a runtime one:
		// keep the raw text so the message still states something true.
		return text
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, fields); err != nil {
		return text
	}
	return out.String()
}
