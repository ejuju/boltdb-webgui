package httputils

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ejuju/boltdb_webgui/pkg/logs"
)

// Access logging middleware logs incoming HTTP requests
func AccessLoggingMiddleware(logger logs.Logger) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resrec := &httpStatusRecorder{ResponseWriter: w} // use custom response writer to record status
			before := time.Now()                             // record timestamp before request is handled
			h.ServeHTTP(resrec, r)                           //
			dur := time.Since(before)                        // calculate duration to handle request

			// Log
			logstr := fmt.Sprintf("%d %-4s %4dμs %s", resrec.statusCode, r.Method, dur.Microseconds(), r.URL.Path)
			logger.Log(logstr)
		})
	}
}

type httpStatusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (srec *httpStatusRecorder) WriteHeader(statusCode int) {
	srec.statusCode = statusCode
	srec.ResponseWriter.WriteHeader(statusCode)
}

type PanicHandler func(w http.ResponseWriter, r *http.Request, err any)

// Panic recovery middleware logs the recovered error and executes the onPanic callback function.
func PanicRecoveryMiddleware(logger logs.Logger, onPanic PanicHandler) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stackstr := strings.ReplaceAll(string(debug.Stack()), "\n", " ")
					stackstr = strings.ReplaceAll(string(stackstr), "\t", " ")
					logger.Log(fmt.Sprintf("%v (%s)", err, stackstr))
					if onPanic != nil {
						onPanic(w, r, err)
					}
				}
			}()

			h.ServeHTTP(w, r)
		})
	}
}
