// SPDX-FileCopyrightText: 2017-2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package logg provides some convenience functions on top of the "log" package
// from the stdlib. It always uses the stdlib's standard logger.
//
// The functions in this package work like log.Println() or like log.Printf()
// depending on whether arguments are passed after the message string:
//
//	import (
//		"log"
//		"github.com/sapcc/go-bits/logg"
//	)
//
//	//The following two are equivalent:
//	logg.Info("starting up")
//	std_log.Println("INFO: starting up")
//
//	//The following two are equivalent:
//	logg.Info("listening on port %d", port)
//	std_log.Printf("INFO: listening on port %d\n", port)
package logg

import (
	stdlog "log"
	"os"
	"strings"
	"sync"
)

var (
	// ShowDebug can be set to true to enable the display of debug logs.
	ShowDebug = false
	log       = stdlog.New(stdlog.Writer(), stdlog.Prefix(), stdlog.Flags())
	mu        sync.Mutex
)

// SetLogger allows to define custom logger
func SetLogger(l *stdlog.Logger) {
	mu.Lock()
	defer mu.Unlock()
	log = l
}

// Fatal logs a fatal error and terminates the program.
func Fatal(msg string, args ...any) {
	doLog("FATAL: "+msg, args)
	os.Exit(1)
}

// Error logs a non-fatal error.
func Error(msg string, args ...any) {
	doLog("ERROR: "+msg, args)
}

// Info logs an informational message.
func Info(msg string, args ...any) {
	doLog("INFO: "+msg, args)
}

// Debug logs a debug message if debug logging is enabled.
func Debug(msg string, args ...any) {
	if ShowDebug {
		doLog("DEBUG: "+msg, args)
	}
}

// Other logs a message with a custom log level.
func Other(level, msg string, args ...any) {
	doLog(level+": "+msg, args)
}

func doLog(msg string, args []any) {
	msg = strings.TrimSpace(msg)               // most importantly, skip trailing '\n'
	msg = strings.ReplaceAll(msg, "\n", "\\n") // avoid multiline log messages
	if len(args) > 0 {
		log.Printf(msg+"\n", args...)
	} else {
		log.Println(msg)
	}
}
