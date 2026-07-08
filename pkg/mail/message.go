package mail

import (
	"fmt"
	"sort"
	"strings"
)

func buildRFC822Message(msg Message) []byte {
	headers := make([]string, 0, 8+len(msg.Headers))
	headers = append(headers, "From: "+strings.TrimSpace(msg.From))
	headers = append(headers, "To: "+strings.Join(cleanRecipients(msg.To), ", "))
	headers = append(headers, "Subject: "+strings.TrimSpace(msg.Subject))
	headers = append(headers, "MIME-Version: 1.0")
	headers = append(headers, "Content-Type: text/plain; charset=UTF-8")

	if len(msg.Headers) > 0 {
		customKeys := make([]string, 0, len(msg.Headers))
		for k := range msg.Headers {
			customKeys = append(customKeys, k)
		}
		sort.Strings(customKeys)
		for _, key := range customKeys {
			value := strings.TrimSpace(msg.Headers[key])
			if value == "" {
				continue
			}
			headers = append(headers, fmt.Sprintf("%s: %s", strings.TrimSpace(key), value))
		}
	}

	lines := append(headers, "", msg.Body)
	return []byte(strings.Join(lines, "\r\n"))
}

func cleanRecipients(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
