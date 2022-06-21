/*******************************************************************************
*
* Copyright 2019 SAP SE
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

// WARNING: This is not a useful program. Read README.md in the parent
// directory to understand what's going on here.

package main

import (
	"database/sql"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	bindata "github.com/golang-migrate/migrate/v4/source/go_bindata"
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
