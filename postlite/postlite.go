/*******************************************************************************
*
* Copyright 2018 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

//Package postlite is a database library for applications that use PostgreSQL
//in production and in-memory SQLite for testing. It imports the necessary SQL
//drivers and integrates github.com/golang-migrate/migrate for data definition.
//When running with SQLite, executed SQL statements are logged with
//logg.Debug() from github.com/sapcc/go-bits/logg.
package postlite

import (
	"database/sql"
	"errors"
	"fmt"
	net_url "net/url"
	"os"
	"regexp"
	"strings"

	"github.com/golang-migrate/migrate"
	"github.com/golang-migrate/migrate/database"
	"github.com/golang-migrate/migrate/database/postgres"
	bindata "github.com/golang-migrate/migrate/source/go_bindata"
	//enable postgres driver for database/sql
	_ "github.com/lib/pq"
)

//Configuration contains settings for Init(). The field Migrations needs to have keys
//matching the filename format expected by github.com/golang-migrate/migrate
//(see documentation there for details), for example:
//
//    cfg.Migrations = map[string]string{
//        "001_initial.up.sql": `
//            BEGIN;
//            CREATE TABLE things (
//                id   BIGSERIAL NOT NULL PRIMARY KEY,
//                name TEXT NOT NULL,
//            );
//            COMMIT;
//        `,
//        "001_initial.down.sql": `
//            BEGIN;
//            DROP TABLE things;
//            COMMIT;
//        `,
//    }
//
type Configuration struct {
	//(required for Postgres, ignored for SQLite) A libpq connection URL, see:
	//<https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING>
	PostgresURL *net_url.URL
	//(required) The schema migrations, in Postgres syntax. See above for details.
	Migrations map[string]string
	//(optional) If not empty, use this database/sql driver instead of "postgres"
	//or "sqlite3-postlite". This is useful e.g. when using github.com/majewsky/sqlproxy.
	OverrideDriverName string
}

//Connect connects to a Postgres database if cfg.PostgresURL is set, or to an
//in-memory SQLite3 database otherwise. Use of SQLite3 is only safe in unit
//tests! Unit tests may not be run in parallel!
func Connect(cfg Configuration) (*sql.DB, error) {
	migrations := stripWhitespace(cfg.Migrations)

	var (
		db               *sql.DB
		dbNameForMigrate string
		err              error
	)
	if cfg.PostgresURL == nil {
		db, err = connectToSQLite(cfg.OverrideDriverName)
		if err != nil {
			return nil, fmt.Errorf("cannot create SQLite in-memory DB: %s", err.Error())
		}
		dbNameForMigrate = "sqlite3"
		migrations = translateSQLiteDDLToPostgres(migrations)
	} else {
		db, err = connectToPostgres(cfg.PostgresURL, cfg.OverrideDriverName)
		if err != nil {
			return nil, fmt.Errorf("cannot connect to Postgres: %s", err.Error())
		}
		dbNameForMigrate = "postgres"
	}

	dbDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err == nil {
		err = migrateSchema(dbNameForMigrate, dbDriver, migrations)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot apply database schema: %s", err.Error())
	}

	return db, nil
}

func connectToSQLite(driverName string) (*sql.DB, error) {
	if driverName == "" {
		driverName = "sqlite3-postlite"
	}
	//see FAQ in go-sqlite3 README about the connection string
	db, err := sql.Open(driverName, "file::memory:?mode=memory&cache=shared")
	if err != nil {
		return nil, err
	}

	//wipe leftovers from previous test runs
	//(courtesy of https://stackoverflow.com/a/548297/334761)
	for _, stmt := range []string{
		"PRAGMA writable_schema = 1;",
		"DELETE FROM sqlite_master WHERE TYPE IN ('table', 'index', 'trigger');",
		"PRAGMA writable_schema = 0;",
		"VACUUM;",
		"PRAGMA INTEGRITY_CHECK;",
	} {
		_, err := db.Exec(stmt)
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}

var dbNotExistErrRx = regexp.MustCompile(`^pq: database "([^"]+)" does not exist$`)

func connectToPostgres(url *net_url.URL, driverName string) (*sql.DB, error) {
	if driverName == "" {
		driverName = "postgres"
	}
	db, err := sql.Open(driverName, url.String())
	if err == nil {
		//apparently the "database does not exist" error only occurs when trying to issue the first statement
		_, err = db.Exec("SELECT 1")
	}
	if err == nil {
		//success
		return db, nil
	}
	match := dbNotExistErrRx.FindStringSubmatch(err.Error())
	if match == nil {
		//unexpected error
		return nil, err
	}
	dbName := match[1]

	//connect to Postgres without the database name specified, so that we can
	//execute CREATE DATABASE
	urlWithoutDB := *url
	urlWithoutDB.Path = "/"
	db2, err := sql.Open("postgres", urlWithoutDB.String())
	if err == nil {
		_, err = db2.Exec("CREATE DATABASE " + dbName)
	}
	if err == nil {
		err = db2.Close()
	} else {
		db2.Close()
	}
	if err != nil {
		return nil, err
	}

	//now the actual database is there and we can connect to it
	return sql.Open("postgres", url.String())
}

func migrateSchema(dbName string, dbDriver database.Driver, sqlMigrations map[string]string) error {
	//use the "go-bindata" driver for github.com/mattes/migrate, but without
	//actually using go-bindata (go-bindata stubbornly insists on making its
	//generated functions public, but I don't want to pollute the API)
	var assetNames []string
	for name := range sqlMigrations {
		assetNames = append(assetNames, name)
	}
	asset := func(name string) ([]byte, error) {
		data, ok := sqlMigrations[name]
		if ok {
			return []byte(data), nil
		}
		return nil, &os.PathError{Op: "open", Path: name, Err: errors.New("not found")}
	}

	sourceDriver, err := bindata.WithInstance(bindata.Resource(assetNames, asset))
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("go-bindata", sourceDriver, dbName, dbDriver)
	if err != nil {
		return err
	}
	err = m.Up()
	if err == migrate.ErrNoChange {
		//no idea why this is an error
		return nil
	}
	return err
}

func stripWhitespace(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for filename, sql := range in {
		out[filename] = strings.Replace(
			strings.Join(strings.Fields(sql), " "),
			"; ", ";\n", -1,
		)
	}
	return out
}
