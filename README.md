# go-bits

[![GoDoc](https://godoc.org/github.com/sapcc/go-bits?status.svg)](https://godoc.org/github.com/sapcc/go-bits)

Some tiny pieces of Go code, extracted from their original applications for
reusability. Feel free to add to this.

## Packages

* [assert](./assert) contains various assertions for unit tests.
* [audittools](./audittools) contains helper functions for establishing a connection to a RabbitMQ server (with sane defaults) and publishing messages to it.
* [easypg](./easypg) is a database library for applications that use PostgreSQL. It integrates [golang-migrate/migrate](https://github.com/golang-migrate/migrate) for data definition and imports the libpq-based SQL driver.
* [gopherpolicy](./gopherpolicy) integrates [Gophercloud](https://github.com/gophercloud/gophercloud) with [goslo.policy](https://github.com/databus23/goslo.policy), for OpenStack services that need to validate client tokens and check permissions.
* [httpapi](./httpapi) contains opinionated base machinery for assembling and exposing an API consisting of HTTP endpoints.
* [httpext](./httpext) adds some convenience functions to [net/http](https://golang.org/pkg/http/).
* [logg](./logg) adds some convenience functions to [log](https://golang.org/pkg/log/).
* [must](./must) contains convenience functions for quickly exiting on fatal errors without the need for excessive `if err != nil`.
* [osext](./osext) contains extensions to the standard library package "os", mostly relating to parsing of environment variables.
* [respondwith](./respondwith) contains some helper functions for generating responses in HTTP handlers.
* [secrets](./secrets) provides convenience functions for working with auth credentials.
* [sre](./sre) contains a HTTP middleware that emits SRE-related Prometheus metrics.
* [sqlext](./sqlext) contains helper functions for SQL queries that are not specific to PostgreSQL.

## Tools

The `tools` subdirectory contains small Go programs.

* [release-info](./tools/release-info) extracts release info for a specific version from a
  changelog file that uses the [Keep a changelog](https://keepachangelog.com) format.
