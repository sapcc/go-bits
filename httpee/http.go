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
	"time"
)

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// ContextWithSIGINT creates a new context.Context using the provided Context, and
// launches a goroutine that cancels the Context when an interrupt signal is caught.
func ContextWithSIGINT(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, shutdownSignals...)
	go func() {
		<-signalChan
		signal.Reset(shutdownSignals...)
		close(signalChan)
		cancel()
	}()
	return ctx
}

// ListenAndServeContext is a wrapper around http.ListenAndServe() that additionally
// shuts down the HTTP server gracefully when the context expires, or if an error occurs.
func ListenAndServeContext(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{Addr: addr, Handler: handler}

	// waitForServerShutdown channel serves two purposes:
	// 1. It is used to block until server.Shutdown() returns to prevent
	// program from exiting prematurely. This is because when Shutdown is
	// called ListenAndServe immediately return ErrServerClosed.
	// 2. It is used to convey errors encountered during Shutdown from the
	// goroutine to the caller function.
	waitForServerShutdown := make(chan error)
	shutdownServer := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-shutdownServer:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := server.Shutdown(ctx)
		cancel()
		waitForServerShutdown <- err
	}()

	listenAndServeErr := server.ListenAndServe()
	if listenAndServeErr != http.ErrServerClosed {
		shutdownServer <- struct{}{}
	}

	shutdownErr := <-waitForServerShutdown
	if listenAndServeErr == http.ErrServerClosed {
		return shutdownErr
	}
	return listenAndServeErr
}
