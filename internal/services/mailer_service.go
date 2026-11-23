package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"mime"
	"net/smtp"
	"strings"
	"time"
)

// MailerService manda los correos de la cuenta: restablecer la contraseña y el código para verificar el correo.
type MailerService interface {
	SendPasswordReset(toEmail, resetURL string) error
	SendVerificationCode(toEmail, name, code string) error
}

// ConsoleMailer imprime los correos en el log (sin SMTP_HOST, en desarrollo).
type ConsoleMailer struct{}

func NewConsoleMailer() *ConsoleMailer {
	return &ConsoleMailer{}
}

func (s *ConsoleMailer) SendPasswordReset(toEmail, resetURL string) error {
	log.Printf("[CONSOLE MAILER] restablecer contraseña · %s · %s", toEmail, resetURL)
	return nil
}

func (s *ConsoleMailer) SendVerificationCode(toEmail, name, code string) error {
	log.Printf("[CONSOLE MAILER] código de verificación · %s · %s", toEmail, code)
	return nil
}

var _ MailerService = (*ConsoleMailer)(nil)

// SMTPMailer manda por SMTP (Google Workspace: smtp.gmail.com:587 con una contraseña de aplicación). User es la cuenta
// que se autentica (admin@) y Sender el remitente que ve el cliente (soporte@, un alias de esa cuenta).
type SMTPMailer struct {
	Host   string
	Port   string
	User   string
	Pass   string
	Sender string
}

func NewSMTPMailer(host, port, user, pass, sender string) *SMTPMailer {
	if sender == "" {
		sender = user
	}
	return &SMTPMailer{Host: host, Port: port, User: user, Pass: pass, Sender: sender}
}

// buildMessage arma un correo HTML con texto plano de respaldo y encabezados en UTF-8 (tildes en el asunto).
func buildMessage(from, to, subject, text, htmlBody string) []byte {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	boundary := "empre-" + hex.EncodeToString(b)
	domain := "empre.com.co"
	if i := strings.LastIndex(from, "@"); i >= 0 {
		domain = from[i+1:]
	}
	var m strings.Builder
	m.WriteString("From: " + mime.QEncoding.Encode("utf-8", "Empre") + " <" + from + ">\r\n")
	m.WriteString("To: <" + to + ">\r\n")
	m.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	m.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	m.WriteString("Message-ID: <" + hex.EncodeToString(b) + "@" + domain + ">\r\n")
	m.WriteString("MIME-Version: 1.0\r\n")
	m.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	m.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=\"UTF-8\"\r\n\r\n" + text + "\r\n")
	m.WriteString("--" + boundary + "\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n\r\n" + htmlBody + "\r\n")
	m.WriteString("--" + boundary + "--\r\n")
	return []byte(m.String())
}

func mailLayout(title, inner string) string {
	return `<!doctype html><html lang="es"><body style="margin:0;background:#F5F5F4;font-family:system-ui,-apple-system,'Segoe UI',Roboto,sans-serif;color:#2B2622">` +
		`<div style="max-width:480px;margin:0 auto;padding:24px 16px"><div style="background:#fff;border-radius:16px;padding:24px">` +
		`<p style="margin:0 0 16px;font-weight:800;color:#D42020;font-size:18px">Empre</p>` +
		`<h1 style="font-size:20px;margin:0 0 12px">` + title + `</h1>` + inner +
		`</div><p style="text-align:center;color:#57534E;font-size:12px;margin-top:16px">Si no fuiste tú, ignora este correo.</p></div></body></html>`
}

func (s *SMTPMailer) send(to, subject, text, htmlBody string) error {
	auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)
	return smtp.SendMail(fmt.Sprintf("%s:%s", s.Host, s.Port), auth, s.User, []string{to}, buildMessage(s.Sender, to, subject, text, htmlBody))
}

func (s *SMTPMailer) SendPasswordReset(toEmail, resetURL string) error {
	u := html.EscapeString(resetURL)
	body := mailLayout("Restablece tu contraseña",
		`<p style="line-height:1.5">Pediste cambiar la contraseña de tu cuenta de Empre. Toca el botón para escribir una nueva. El enlace sirve por 1 hora.</p>`+
			`<p style="margin:24px 0"><a href="`+u+`" style="background:#D42020;color:#fff;text-decoration:none;padding:12px 20px;border-radius:12px;font-weight:700;display:inline-block">Cambiar mi contraseña</a></p>`+
			`<p style="font-size:12px;color:#57534E;word-break:break-all">O abre este enlace: `+u+`</p>`)
	text := "Pediste cambiar la contraseña de tu cuenta de Empre. Abre este enlace (sirve por 1 hora): " + resetURL + "\r\n\r\nSi no fuiste tú, ignora este correo."
	return s.send(toEmail, "Restablece tu contraseña de Empre", text, body)
}

func (s *SMTPMailer) SendVerificationCode(toEmail, name, code string) error {
	hello := "Hola"
	if f := strings.Fields(name); len(f) > 0 {
		hello = "Hola " + html.EscapeString(f[0])
	}
	body := mailLayout("Tu código de Empre",
		`<p style="line-height:1.5">`+hello+`, escribe este código en la app para verificar tu correo. Sirve por 30 minutos.</p>`+
			`<p style="font-size:32px;font-weight:800;letter-spacing:8px;margin:24px 0;text-align:center">`+code+`</p>`)
	text := fmt.Sprintf("%s, tu código para verificar tu correo en Empre es %s. Sirve por 30 minutos.\r\n\r\nSi no fuiste tú, ignora este correo.", strings.ReplaceAll(hello, "&#39;", "'"), code)
	return s.send(toEmail, "Tu código de Empre: "+code, text, body)
}

var _ MailerService = (*SMTPMailer)(nil)
