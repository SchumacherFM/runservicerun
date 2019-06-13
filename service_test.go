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

package runservicerun_test

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SchumacherFM/runservicerun"
	"github.com/fortytw2/leaktest"
)

type mutextBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (mb *mutextBuffer) Write(p []byte) (n int, err error) {
	mb.mu.Lock()
	n, err = mb.buf.Write(p)
	mb.mu.Unlock()
	return
}

func (mb *mutextBuffer) String() string {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.buf.String()
}

func (mb *mutextBuffer) log(msg string, args ...interface{}) {
	fmt.Fprintf(&mb.buf, msg+"\n", args...)
}

func TestGoHappyPath(t *testing.T) {
	defer leaktest.CheckTimeout(t, 600*time.Millisecond)()

	logBuf := &mutextBuffer{}
	logFn := func(msg string, args ...interface{}) {
		fmt.Fprintf(logBuf, msg+"\n", args...)
	}

	go func() {
		nullHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

		err := runservicerun.Go(runservicerun.Options{
			Signals:  []os.Signal{syscall.SIGUSR1},
			LogError: logFn,
			LogInfo:  logFn,
		},
			runservicerun.WithHTTPHandler(":7878", nullHandler),
			runservicerun.WithHTTPServer(&http.Server{
				Addr:    ":7879",
				Handler: nullHandler,
			}),
			runservicerun.WithHTTPHandlerTLS(":7880", "testdata/cert.crt", "testdata/key.pem", &tls.Config{InsecureSkipVerify: true}, nullHandler),
			runservicerun.WithHTTPServerTLS("testdata/cert.crt", "testdata/key.pem", &http.Server{
				Addr:      ":7881",
				Handler:   nullHandler,
				TLSConfig: &tls.Config{InsecureSkipVerify: true},
			}),
			runservicerun.WithCloserBefore("testCloserB", ioutil.NopCloser(nil)),
			runservicerun.WithCloserAfter("testCloserA", ioutil.NopCloser(nil)),
			runservicerun.WithStartFunc("testStart", func() error { return nil }),
		)
		if err != nil {
			t.Fatal(err)
		}
	}()

	killAndCheckLog(t, logBuf, `starting "testStart"`,
		`starting ListenAndServe at ":7878"`,
		`starting ListenAndServe at ":7879"`,
		`starting ListenAndServeTLS at ":7880"`,
		`starting ListenAndServeTLS at ":7881"`,
		`received signal: user defined signal 1`,
		`closing before: "testCloserB"`,
		`shutting down server :7878`,
		`shutting down server :7879`,
		`shutting down server :7880`,
		`shutting down server :7881`,
		`closing after: "testCloserA"`)
}

type closeErr struct {
	err error
}

func (c closeErr) Close() error {
	return c.err
}

func TestGoShutdownCloseError(t *testing.T) {
	defer leaktest.CheckTimeout(t, 600*time.Millisecond)()

	logBuf := &mutextBuffer{}
	logFn := func(msg string, args ...interface{}) {
		fmt.Fprintf(logBuf, msg+"\n", args...)
	}

	go func() {
		err := runservicerun.Go(runservicerun.Options{
			Signals:  []os.Signal{syscall.SIGUSR1},
			LogError: logFn,
			LogInfo:  logFn,
		},
			runservicerun.WithCloserBefore("testCloserB", closeErr{err: errors.New("error close before")}),
			runservicerun.WithCloserAfter("testCloserA", closeErr{err: errors.New("error close after")}),
		)
		if err == nil {
			t.Fatal("Expected an error in go routine running runservicerun.Go")
		}
		if have, want := err.Error(), "error close before"; have != want {
			t.Errorf("\nHave: %s\nWant: %s", have, want)
		}
	}()

	killAndCheckLog(t, logBuf, `received signal: user defined signal 1`,
		`closing before: "testCloserB"`,
		`service "testCloserB" failed to close with error: error close before`,
		`closing after: "testCloserA"`,
		`service "testCloserA" failed to close with error: error close after`)
}

func TestGoStartFnFailed(t *testing.T) {
	defer leaktest.CheckTimeout(t, 600*time.Millisecond)()

	logBuf := &mutextBuffer{}
	logFn := func(msg string, args ...interface{}) {
		fmt.Fprintf(logBuf, msg+"\n", args...)
	}

	nullHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	err := runservicerun.Go(runservicerun.Options{
		Signals:  []os.Signal{syscall.SIGUSR1},
		LogError: logFn,
		LogInfo:  logFn,
	},
		runservicerun.WithHTTPHandler(":7878", nullHandler),
		runservicerun.WithStartFunc("testStart", func() error { return errors.New("startFn failed") }),
	)
	if err == nil {
		t.Fatal("expected an error during start")
	}
	if have, want := err.Error(), "startFn failed"; have != want {
		t.Errorf("\nHave: %s\nWant: %s", have, want)
	}

	killAndCheckLog(t, logBuf, `starting "testStart"`,
		`context canceled, closing signal goroutine`,
		`shutting down server :7878`,
		`starting ListenAndServe at ":7878"`)
}

func killAndCheckLog(t *testing.T, logStr fmt.Stringer, wantLogLines ...string) {
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	t.Log(logStr)
	for _, l := range wantLogLines {
		if !strings.Contains(logStr.String(), l) {
			t.Fatalf("%s\n\ndoes not contain: %s", logStr.String(), l)
		}
	}
}
