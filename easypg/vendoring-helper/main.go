// WARNING: This is not a useful program. Read README.md in the parent
// directory to understand what's going on here.

package main

import (
	"database/sql"

	"github.com/golang-migrate/migrate"
	"github.com/golang-migrate/migrate/database/postgres"
	bindata "github.com/golang-migrate/migrate/source/go_bindata"
)

func main() {
	var (
		assetNames []string
		asset      func(string) ([]byte, error)
		db         *sql.DB
	)
	b, _ := bindata.WithInstance(bindata.Resource(assetNames, asset))
	p, _ := postgres.WithInstance(db, &postgres.Config{})
	m, _ := migrate.NewWithInstance("go-bindata", b, "postgres", p)
	_ = m.Up()
}
