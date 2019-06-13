# runservicerun (for the Go programming language)

Run Service Run! Starts multiple *http.Server (or other services) and terminates
on os.Signal with a graceful shutdown. 🎽

```go
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
		runservicerun.WithStartFunc("testStart", func() error { println("Started something") return nil }),
	)
	if err != nil {
		panic(err) // or any other way to handle this error
	}
```

# Contribute

Send me a pull request or open an issue if you encounter a bug or something can
be improved!

Multi-time pull request senders gets collaborator access.

# License

[Cyrill Schumacher](https://github.com/SchumacherFM) - [My pgp public key](https://www.schumacher.fm/cyrill.asc)

Copyright 2016 Cyrill Schumacher All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
