package services

import (
	"fmt"
	"log"
	"net/smtp"
)

type MailerService interface {
	SendPasswordReset(toEmail, resetURL string) error
}

type ConsoleMailer struct{}

func NewConsoleMailer() *ConsoleMailer {
	return &ConsoleMailer{}
}

func (s *ConsoleMailer) SendPasswordReset(toEmail, resetURL string) error {
	log.Printf("\n--- [CONSOLE MAILER] ---\nTO: %s\nSUBJECT: Password Reset Request\nBODY: Click here to reset your password: %s\n------------------------\n", toEmail, resetURL)
	return nil
}

// Ensure ConsoleMailer implements MailerService
var _ MailerService = (*ConsoleMailer)(nil)

type SMTPMailer struct {
	Host   string
	Port   string
	User   string
	Pass   string
	Sender string
}

func NewSMTPMailer(host, port, user, pass, sender string) *SMTPMailer {
	return &SMTPMailer{
		Host:   host,
		Port:   port,
		User:   user,
		Pass:   pass,
		Sender: sender,
	}
}

func (s *SMTPMailer) SendPasswordReset(toEmail, resetURL string) error {
	subject := "Subject: Password Reset Request\r\n"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n"
	body := fmt.Sprintf("<html><body><h3>Password Reset Request</h3><p>Click the link below to reset your password:</p><p><a href=\"%s\">%s</a></p><p>This link will expire in 1 hour.</p></body></html>", resetURL, resetURL)
	msg := []byte(subject + mime + body)

	auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)
	addr := fmt.Sprintf("%s:%s", s.Host, s.Port)

	return smtp.SendMail(addr, auth, s.Sender, []string{toEmail}, msg)
}

// Ensure SMTPMailer implements MailerService
var _ MailerService = (*SMTPMailer)(nil)
