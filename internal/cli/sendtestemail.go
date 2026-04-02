package cli

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type smtpSendConfig struct {
	host string
	port int
	user string
	pass string
}

func runSendTestEmail(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sendtestemail", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	toRaw := fs.String("to", "", "Comma-separated recipient emails")
	from := fs.String("from", "", "Sender email (defaults to config mail_from)")
	subject := fs.String("subject", "GoFrame test email", "Subject line")
	body := fs.String("body", "This is a test email sent by goframe sendtestemail.", "Email body")
	timeout := fs.Duration("timeout", 10*time.Second, "SMTP operation timeout")
	dryRun := fs.Bool("dry-run", false, "Print send plan without opening an SMTP connection")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	recipients, err := collectSendTestEmailRecipients(*toRaw, fs.Args())
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return fmt.Errorf("at least one recipient is required (use --to or positional emails)")
	}
	for _, recipient := range recipients {
		if err := validateEmail(recipient); err != nil {
			return err
		}
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	fromAddr := strings.TrimSpace(*from)
	if fromAddr == "" {
		fromAddr = strings.TrimSpace(cfg.MailFrom)
	}
	if fromAddr == "" {
		return fmt.Errorf("mail_from is required (set --from or config mail_from)")
	}
	if err := validateEmail(fromAddr); err != nil {
		return fmt.Errorf("invalid sender email: %w", err)
	}

	sub := strings.TrimSpace(*subject)
	if sub == "" {
		return fmt.Errorf("subject cannot be empty")
	}
	msgBody := *body
	if strings.TrimSpace(msgBody) == "" {
		return fmt.Errorf("body cannot be empty")
	}

	if *timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}

	smtpCfg := smtpSendConfig{
		host: strings.TrimSpace(cfg.SMTPHost),
		port: cfg.SMTPPort,
		user: strings.TrimSpace(cfg.SMTPUser),
		pass: cfg.SMTPPass,
	}
	if !*dryRun {
		if smtpCfg.host == "" {
			return fmt.Errorf("smtp_host is required in config for sendtestemail")
		}
		if smtpCfg.port <= 0 {
			return fmt.Errorf("smtp_port must be greater than 0")
		}
	}

	if *dryRun {
		fmt.Fprintf(
			stdout,
			"DRY-RUN\tSENDTESTEMAIL\tfrom=%s\tto=%s\tsubject=%q\thost=%s\tport=%d\ttimeout=%s\n",
			fromAddr,
			strings.Join(recipients, ","),
			sub,
			smtpCfg.host,
			smtpCfg.port,
			timeout.String(),
		)
		return nil
	}

	payload := buildSMTPMessage(fromAddr, recipients, sub, msgBody)
	if err := sendSMTPMessage(smtpCfg, fromAddr, recipients, payload, *timeout); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Test email sent: from=%s to=%s subject=%q\n", fromAddr, strings.Join(recipients, ","), sub)
	return nil
}

func collectSendTestEmailRecipients(toFlag string, positional []string) ([]string, error) {
	joined := make([]string, 0, len(positional)+1)
	if strings.TrimSpace(toFlag) != "" {
		joined = append(joined, toFlag)
	}
	joined = append(joined, positional...)

	seen := make(map[string]struct{}, len(joined))
	out := make([]string, 0, len(joined))

	for _, chunk := range joined {
		for _, token := range strings.Split(chunk, ",") {
			email := strings.TrimSpace(token)
			if email == "" {
				continue
			}
			lower := strings.ToLower(email)
			if _, exists := seen[lower]; exists {
				continue
			}
			seen[lower] = struct{}{}
			out = append(out, email)
		}
	}
	return out, nil
}

func buildSMTPMessage(from string, recipients []string, subject, body string) []byte {
	lines := []string{
		"From: " + from,
		"To: " + strings.Join(recipients, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}
	return []byte(strings.Join(lines, "\r\n"))
}

func sendSMTPMessage(cfg smtpSendConfig, from string, recipients []string, payload []byte, timeout time.Duration) error {
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("connect SMTP server %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("start SMTP client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: cfg.host}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("starttls failed: %w", err)
		}
	}

	if cfg.user != "" {
		auth := smtp.PlainAuth("", cfg.user, cfg.pass, cfg.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL FROM failed: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp RCPT TO failed for %s: %w", recipient, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA failed: %w", err)
	}
	if _, err := wc.Write(payload); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write smtp payload: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("finalize smtp payload: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit failed: %w", err)
	}
	return nil
}
