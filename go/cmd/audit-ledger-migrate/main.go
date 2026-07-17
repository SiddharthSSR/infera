package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/infera/infera/go/internal/audit"
)

func main() {
	sqlitePath := flag.String("sqlite", "data/audit.db", "path to the drained SQLite audit ledger")
	postgresDSN := flag.String("postgres-dsn", os.Getenv("INFERA_AUDIT_LEDGER_DSN"), "target PostgreSQL DSN")
	flag.Parse()
	if strings.TrimSpace(*postgresDSN) == "" {
		fmt.Fprintln(os.Stderr, "-postgres-dsn or INFERA_AUDIT_LEDGER_DSN is required")
		os.Exit(2)
	}
	target, err := audit.NewPostgresStore(*postgresDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open target ledger: %v\n", err)
		os.Exit(1)
	}
	defer target.Close()
	copied, err := target.MigrateSQLiteHistory(context.Background(), *sqlitePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration failed after %d rows: %v\n", copied, err)
		os.Exit(1)
	}
	fmt.Printf("migration complete: verified %d SQLite audit rows in PostgreSQL\n", copied)
}
