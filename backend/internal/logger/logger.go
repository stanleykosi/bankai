/**
 * @description
 * Structured logger for Bankai Backend.
 * Ensures info messages go to stdout (not stderr) so Railway doesn't label them as errors.
 *
 * @dependencies
 * - standard "os"
 * - standard "log"
 * - standard "fmt"
 */

package logger

import (
	"fmt"
	"io"
	"log"
	"os"
)

var (
	// InfoLogger writes to stdout (Railway won't label as errors)
	InfoLogger *log.Logger
	// ErrorLogger writes to stderr (for actual errors)
	ErrorLogger *log.Logger
)

func init() {
	// Info logs go to stdout - Railway will parse these correctly
	InfoLogger = log.New(os.Stdout, "", 0)
	// Error logs go to stderr - Railway will correctly identify these as errors
	ErrorLogger = log.New(os.Stderr, "", 0)
}

// Info logs an info message to stdout
func Info(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	InfoLogger.Println(message)
}

// Error logs an error message to stderr
func Error(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	ErrorLogger.Println(message)
}

// Fatal logs an error and exits
func Fatal(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	ErrorLogger.Fatalln(message)
}

// New creates a new logger that writes to the specified writer
func New(w io.Writer) *log.Logger {
	return log.New(w, "", 0)
}

