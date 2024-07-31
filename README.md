# go-bits

[![GoDoc](https://godoc.org/github.com/sapcc/go-bits?status.svg)](https://godoc.org/github.com/sapcc/go-bits)

Some tiny pieces of Go code, extracted from their original applications for
reusability. Feel free to add to this.

## Packages

* [assert](./assert) contains various assertions for unit tests.
* [audittools](./audittools) contains helper functions for establishing a connection to a RabbitMQ server (with sane defaults) and publishing messages to it.
* [easypg](./easypg) is a database library for applications that use PostgreSQL. It integrates [golang-migrate/migrate](https://github.com/golang-migrate/migrate) for data definition and imports the libpq-based SQL driver.
* [errext](./errext) contains convenience functions for handling and propagating errors.
* [gopherpolicy](./gopherpolicy) integrates [Gophercloud](https://github.com/gophercloud/gophercloud) with [goslo.policy](https://github.com/databus23/goslo.policy), for OpenStack services that need to validate client tokens and check permissions.
* [httpapi](./httpapi) contains opinionated base machinery for assembling and exposing an API consisting of HTTP endpoints.
* [httpext](./httpext) adds some convenience functions to [net/http](https://golang.org/pkg/http/).
* [jobloop](./jobloop) contains the Job trait, which abstracts over reusable implementations of worker loops.
* [liquidapi](./liquidapi) is a server runtime for microservices implementing [LIQUID API](https://pkg.go.dev/github.com/sapcc/go-api-declarations/liquid).
* [logg](./logg) adds some convenience functions to [log](https://golang.org/pkg/log/).
* [mock](./mock) contains basic mocks and test doubles.
* [must](./must) contains convenience functions for quickly exiting on fatal errors without the need for excessive `if err != nil`.
* [osext](./osext) contains extensions to the standard library package "os", mostly relating to parsing of environment variables.
* [pluggable](./pluggable) is a tiny plugin factory library, for constructing different objects implementing a common interface based on a configurable type selector.
* [promquery](./promquery) provides a simplified interface for executing Prometheus queries.
* [regexpext](./regexpext) contains convenience functions for marshalling regexes to and from string values in YAML and JSON documents.
* [respondwith](./respondwith) contains some helper functions for generating responses in HTTP handlers.
* [secrets](./secrets) provides convenience functions for working with auth credentials.
* [sqlext](./sqlext) contains helper functions for SQL queries that are not specific to PostgreSQL.
* [vault](./vault) contains helper functions to work with HashiCorp Vault.

## Tools

The `tools` subdirectory contains small Go programs.

* [release-info](./tools/release-info) extracts release info for a specific version from a
  changelog file that uses the [Keep a changelog](https://keepachangelog.com) format.
