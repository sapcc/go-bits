# go-bits

[![GoDoc](https://godoc.org/github.com/sapcc/go-bits?status.svg)](https://godoc.org/github.com/sapcc/go-bits)

Some tiny pieces of Go code, extracted from their original applications for
reusability. Each subdirectory is its own individual package. Feel free to add
to this.

* [assert](./assert) contains various assertions for unit tests.
* [audittools](./audittools) contains helper functions for establishing a connection to a RabbitMQ server (with sane defaults) and publishing messages to it.
* [easypg](./postlite) is a database library for applications that use PostgreSQL. It integrates [golang-migrate/migrate](https://github.com/golang-migrate/migrate) for data definition and imports the libpq-based SQL driver.
* [gopherpolicy](./gopherpolicy) integrates [Gophercloud](https://github.com/gophercloud/gophercloud) with [goslo.policy](https://github.com/databus23/goslo.policy), for OpenStack services that need to validate client tokens and check permissions.
* [httpee](./httpee) adds some convenience functions to [http](https://golang.org/pkg/http/).
* [logg](./logg) adds some convenience functions to [log](https://golang.org/pkg/log/).
* [respondwith](./respondwith) contains some helper functions for generating responses in HTTP handlers.
* [retry](./retry) contains helper methods for creating retry loops using different strategies.
