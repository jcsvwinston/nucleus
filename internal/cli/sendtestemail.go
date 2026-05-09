package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/mail"
)

func runSendTestEmail(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sendtestemail", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	toRaw := fs.String("to", "", "Comma-separated recipient emails")
	from := fs.String("from", "", "Sender email (defaults to config mail_from)")
	driverOverride := fs.String("driver", "", "Mail driver override (defaults to config mail_driver)")
	subject := fs.String("subject", "Nucleus test email", "Subject line")
	body := fs.String("body", "This is a test email sent by nucleus sendtestemail.", "Email body")
	timeout := fs.Duration("timeout", 10*time.Second, "Mail provider operation timeout")
	dryRun := fs.Bool("dry-run", false, "Print send plan without contacting the mail provider")

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

	driver := resolveMailDriver(*driverOverride)
	if strings.TrimSpace(*driverOverride) == "" {
		driver = resolveMailDriver(cfg.MailDriver)
	}

	if *dryRun {
		providerDetails := sendTestEmailProviderDetails(driver, cfg)
		fmt.Fprintf(
			stdout,
			"DRY-RUN\tSENDTESTEMAIL\tdriver=%s\tfrom=%s\tto=%s\tsubject=%q\tprovider=%s\ttimeout=%s\n",
			driver,
			fromAddr,
			strings.Join(recipients, ","),
			sub,
			providerDetails,
			timeout.String(),
		)
		return nil
	}

	if driver == "noop" {
		return fmt.Errorf("mail_driver is noop; configure smtp/sendgrid or install nucleus-plugin-<driver> on PATH")
	}

	sender, err := mail.NewSender(mail.Config{
		Driver:           driver,
		Timeout:          *timeout,
		SMTPHost:         strings.TrimSpace(cfg.SMTPHost),
		SMTPPort:         cfg.SMTPPort,
		SMTPUser:         strings.TrimSpace(cfg.SMTPUser),
		SMTPPass:         cfg.SMTPPass,
		SendGridAPIKey:   strings.TrimSpace(cfg.SendGridAPIKey),
		SendGridEndpoint: strings.TrimSpace(cfg.SendGridEndpoint),
	})
	if err != nil {
		return fmt.Errorf("configure mail driver %q: %w", driver, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := sender.Send(ctx, mail.Message{
		From:    fromAddr,
		To:      recipients,
		Subject: sub,
		Body:    msgBody,
	}); err != nil {
		return fmt.Errorf("send test email via %s: %w", driver, err)
	}

	fmt.Fprintf(stdout, "Test email sent: driver=%s from=%s to=%s subject=%q\n", driver, fromAddr, strings.Join(recipients, ","), sub)
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

func resolveMailDriver(raw string) string {
	driver := strings.ToLower(strings.TrimSpace(raw))
	if driver == "" {
		return "noop"
	}
	return driver
}

func sendTestEmailProviderDetails(driver string, cfg *app.Config) string {
	if cfg == nil {
		return "-"
	}
	switch driver {
	case "smtp":
		host := strings.TrimSpace(cfg.SMTPHost)
		if host == "" {
			host = "-"
		}
		return fmt.Sprintf("smtp_host=%s,smtp_port=%d", host, cfg.SMTPPort)
	case "sendgrid":
		endpoint := strings.TrimSpace(cfg.SendGridEndpoint)
		if endpoint == "" {
			endpoint = "https://api.sendgrid.com/v3/mail/send"
		}
		return fmt.Sprintf("sendgrid_endpoint=%s", endpoint)
	default:
		return fmt.Sprintf("plugin=nucleus-plugin-%s", driver)
	}
}
