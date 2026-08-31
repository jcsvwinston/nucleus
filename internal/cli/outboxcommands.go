package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// runOutbox dispatches the outbox maintenance subcommands (NF-8). Today
// that is `requeue`; inspection lives in `nucleus doctor --check outbox`.
func runOutbox(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printOutboxUsage(stdout)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "requeue":
		return runOutboxRequeue(rest, stdout, stderr)
	case "help", "-h", "--help":
		printOutboxUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown outbox subcommand %q (available: requeue)", sub)
	}
}

func printOutboxUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: nucleus outbox <subcommand>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  requeue [id ...]   Return failed messages to pending so the dispatcher retries them.")
	fmt.Fprintln(w, "                     With no ids, every failed message is requeued; ids that are not")
	fmt.Fprintln(w, "                     currently failed are left untouched. Attempts reset to 0.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Inspect counts with: nucleus doctor --check outbox")
}

type outboxRequeueReport struct {
	Status   string `json:"status"`
	Requeued int64  `json:"requeued"`
	Table    string `json:"table"`
}

// runOutboxRequeue implements `nucleus outbox requeue` — the operational
// remedy NF-8 found missing: a message that exhausts its retries stays
// `failed` forever and the only path back used to be hand-written SQL.
func runOutboxRequeue(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("outbox requeue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "Path to nucleus config file")
	jsonOutput := fs.Bool("json", false, "Output results as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()

	loadedCfg, database, cleanup, err := newDatabase(*configPath)
	if err != nil {
		return fmt.Errorf("outbox requeue: open database: %w", err)
	}
	defer cleanup()

	if !loadedCfg.Outbox.Enabled {
		return fmt.Errorf("outbox requeue: the outbox is disabled in configuration (outbox.enabled: false) — there is no outbox table to requeue from")
	}

	sqlDB, err := database.SqlDB()
	if err != nil {
		return fmt.Errorf("outbox requeue: SQL handle unavailable: %w", err)
	}
	dbCfg, _ := loadedCfg.DatabaseByAlias(loadedCfg.DefaultDatabaseAlias())
	store, err := outbox.NewStore(sqlDB, outbox.Config{
		TableName:   loadedCfg.Outbox.TableName,
		DatabaseURL: dbCfg.URL,
	})
	if err != nil {
		return fmt.Errorf("outbox requeue: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	requeued, err := store.RequeueFailed(ctx, ids...)
	if err != nil {
		return fmt.Errorf("outbox requeue: %w", err)
	}

	table := strings.TrimSpace(loadedCfg.Outbox.TableName)
	if table == "" {
		table = outbox.DefaultTableName
	}

	if *jsonOutput {
		report := outboxRequeueReport{Status: "ok", Requeued: requeued, Table: table}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if requeued == 0 {
		if len(ids) > 0 {
			fmt.Fprintf(stdout, "No messages requeued: none of the given ids is in the 'failed' state in %q.\n", table)
		} else {
			fmt.Fprintf(stdout, "No failed messages in %q — nothing to requeue.\n", table)
		}
		return nil
	}
	fmt.Fprintf(stdout, "Requeued %d message(s) in %q: status reset to 'pending' with a fresh retry budget. The running dispatcher will pick them up on its next poll.\n", requeued, table)
	return nil
}
