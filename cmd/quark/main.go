package main

import (
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/GoFrame/pkg/quark/cli/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
