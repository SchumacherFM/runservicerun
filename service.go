// Copyright 2019 Cyrill @ Schumacher.fm
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package runservicerun starts and gracefully shuts down on os.Signal various
// http.Server and other services.
package runservicerun

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
)

// WithHTTPHandler starts and shutdowns the handler at the address.
func WithHTTPHandler(addr string, handler http.Handler) Config {
	return func(s *services) error {
		s.httpServer = append(s.httpServer, &httpServer{
			Server: &http.Server{
				Addr:    addr,
				Handler: handler,
			},
		})
		return nil
	}
}

// WithHTTPServer starts and shutdowns the given http.Server.
func WithHTTPServer(hs *http.Server) Config {
	return func(s *services) error {
		s.httpServer = append(s.httpServer, &httpServer{
			Server: hs,
		})
		return nil
	}
}

// WithHTTPHandlerTLS starts and shutdowns the handler as TLS server at the
// address.
func WithHTTPHandlerTLS(addr, certFile, keyFile string, tlsConfig *tls.Config, handler http.Handler) Config {
	return func(s *services) error {
		s.httpServer = append(s.httpServer, &httpServer{
			Server: &http.Server{
				TLSConfig: tlsConfig,
				Addr:      addr,
				Handler:   handler,
			},
			CertFile: certFile,
			KeyFile:  keyFile,
		})
		return nil
	}
}

// WithHTTPServerTLS starts and shutdowns the http.Server as TLS server. Make
// sure that http.Server.TLSConfig is set.
func WithHTTPServerTLS(certFile, keyFile string, hs *http.Server) Config {
	return func(s *services) error {
		s.httpServer = append(s.httpServer, &httpServer{
			Server:   hs,
			CertFile: certFile,
			KeyFile:  keyFile,
		})
		return nil
	}
}

// WithCloserBefore calls the Closer before shutting down the servers.
func WithCloserBefore(name string, c io.Closer) Config {
	return func(s *services) error {
		s.closersBefore = append(s.closersBefore, named{name: name, Closer: c})
		return nil
	}
}

// WithCloserAfter calls the Closer after shutting down the servers.
func WithCloserAfter(name string, c io.Closer) Config {
	return func(s *services) error {
		s.closersAfter = append(s.closersAfter, named{name: name, Closer: c})
		return nil
	}
}

// WithStartFunc starts the function in its own go routine.
func WithStartFunc(name string, fn func() error) Config {
	return func(s *services) error {
		s.starts = append(s.starts, named{name: name, startFn: fn})
		return nil
	}
}

type httpServer struct {
	CertFile, KeyFile string
	*http.Server
}

// Config configures the function Go to start and stop servers/services.
type Config func(*services) error

type named struct {
	name string
	io.Closer
	startFn func() error
}

type services struct {
	httpServer    []*httpServer
	closersBefore []named
	closersAfter  []named
	starts        []named
}

// Options use in function Go to apply various optional settings.
type Options struct {
	Context  context.Context
	Signals  []os.Signal
	LogInfo  func(format string, args ...interface{})
	LogError func(format string, args ...interface{})
}

// Go starts the listed servers/services and terminates them gracefully when
// receiving a (default) SIGINT/TERM/KILL os.Signal.
func Go(opt Options, configs ...Config) error {
	if opt.LogInfo == nil {
		opt.LogInfo = func(string, ...interface{}) {}
	}
	if opt.LogError == nil {
		opt.LogError = func(string, ...interface{}) {}
	}
	if opt.Context == nil {
		opt.Context = context.Background()
	}
	if len(opt.Signals) == 0 {
		opt.Signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL}
	}

	var runSrvs services
	for _, srvFn := range configs {
		if err := srvFn(&runSrvs); err != nil {
			return err
		}
	}

	ctx, done := context.WithCancel(opt.Context)
	g, gctx := errgroup.WithContext(ctx)

	// goroutine to check for signals to gracefully finish all functions
	g.Go(func() (gErr error) {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, opt.Signals...)

		defer func() {
			for _, c := range runSrvs.closersBefore {
				opt.LogInfo("closing before: %q", c.name)
				if err := c.Close(); err != nil && err != io.EOF {
					opt.LogError("service %q failed to close with error: %s", c.name, err)
					if gErr == nil {
						gErr = err
					}
				}
			}

			for _, srv := range runSrvs.httpServer {
				opt.LogInfo("shutting down server %s", srv.Addr)
				if err := srv.Shutdown(gctx); err != nil {
					opt.LogError("service %s failed to shutdown with error: %s", srv.Addr, err)
					if gErr == nil {
						gErr = err
					}
				}
			}
			for _, c := range runSrvs.closersAfter {
				opt.LogInfo("closing after: %q", c.name)
				if err := c.Close(); err != nil && err != io.EOF {
					opt.LogError("service %q failed to close with error: %s", c.name, err)
					if gErr == nil {
						gErr = err
					}
				}
			}
		}()

		select {
		case sig := <-sigChan:
			opt.LogInfo("received signal: %s", sig)
			signal.Stop(sigChan)
			done()
		case <-gctx.Done():
			opt.LogInfo("context canceled, closing signal goroutine")
			return gctx.Err()
		}
		return nil
	})

	for _, srv := range runSrvs.httpServer {
		srv := srv
		g.Go(func() error {
			if srv.TLSConfig != nil && srv.CertFile != "" && srv.KeyFile != "" {
				opt.LogInfo("starting ListenAndServeTLS at %q", srv.Addr)
				if err := srv.ListenAndServeTLS(srv.CertFile, srv.KeyFile); err != nil && err != http.ErrServerClosed {
					return err
				}
				return nil
			}
			opt.LogInfo("starting ListenAndServe at %q", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		})
	}

	for _, srv := range runSrvs.starts {
		srv := srv
		g.Go(func() error {
			opt.LogInfo("starting %q", srv.name)
			if err := srv.startFn(); err != nil && err != http.ErrServerClosed && err != io.EOF {
				return err
			}
			return nil
		})
	}

	return g.Wait()
}
