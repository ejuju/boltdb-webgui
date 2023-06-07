package logs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Logger interface {
	Log(s string)
}

// returns the filename and line of a calling function in the stack.
func getStackLevel(offset int) string {
	_, f, line, ok := runtime.Caller(offset)
	if !ok {
		panic(errors.New("invalid runtime caller offset"))
	}
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	fpath, err := filepath.Rel(dir, f)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s:%d", fpath, line)
}

// Logger implementation
// Using the standard library.

type TextLogger struct {
	mutex   *sync.Mutex
	writers []io.Writer
}

func NewTextLogger(writers ...io.Writer) *TextLogger {
	if len(writers) == 0 {
		writers = []io.Writer{os.Stderr}
	}
	return &TextLogger{writers: writers, mutex: &sync.Mutex{}}
}

func (l *TextLogger) Log(s string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	for _, w := range l.writers {
		timestr := time.Now().Format("2006-01-02 15:04:05.000 Z07:00")
		logstr := fmt.Sprintf("%s (%s) %q\n", timestr, getStackLevel(2), s)
		_, err := w.Write([]byte(logstr))
		if err != nil {
			panic(err)
		}
	}
}
