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

//Package httpee provides some convenience functions on top of the "http"
//package from the stdlib.
package httpee

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sapcc/go-bits/logg"
)

// ContextWithSIGINT creates a new context.Context using the provided Context, and
// launches a goroutine that cancels the Context when an interrupt signal is caught.
func ContextWithSIGINT(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalChan
		signal.Reset(os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		close(signalChan)
		cancel()
	}()
	return ctx
}

// ListenAndServeContext is a wrapper around http.ListenAndServe() that additionally
// shuts down the HTTP server gracefully when an error occurs or when an interrupt
// signal is caught.
func ListenAndServeContext(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{Addr: addr, Handler: handler}

	//waitForServerShutdown channel is used to block until server.Shutdown()
	//has returned. This is because when Shutdown is called, Serve, ListenAndServe,
	//and ListenAndServeTLS immediately return ErrServerClosed.
	//We block to make sure the program doesn't exit prematurely.
	waitForServerShutdown := make(chan struct{})
	shutdownServer := make(chan int, 1)
	go func() {
		ctx = ContextWithSIGINT(ctx)
		select {
		case <-shutdownServer:
			//continue down below
		case <-ctx.Done():
			logg.Error("Interrupt received...")
		}

		logg.Error("Shutting down HTTP server")
		err := server.Shutdown(ctx)
		if err != nil {
			logg.Error("HTTP server shutdown: %s", err.Error())
		}
		close(waitForServerShutdown)
	}()

	err := server.ListenAndServe()
	if err != http.ErrServerClosed {
		shutdownServer <- 1
	}

	<-waitForServerShutdown

	return err
}
