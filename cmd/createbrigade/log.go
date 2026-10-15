package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

var LogTag = setLogTag()

const defaultLogTag = "createbrigade"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

const fatalString = `{
	"code" : 500,
	"desc" : "Internal Server Error",
	"status" : "error",
	"message" : "%s"
}`

const closedString = `{
	"code" : 423,
	"desc" : "Locked",
	"status" : "error",
	"message" : "%s"
}`

// closed answers that free brigade registration is temporarily closed.
// Unlike fatal it returns normally: SSH clients then get this JSON answer
// with its code instead of a bare non-zero exit status.
func closed(w io.Writer, jout bool, msg string) {
	switch jout {
	case true:
		fmt.Fprintf(w, closedString, msg)
	default:
		fmt.Fprint(w, msg)
	}

	log.Print(msg)
}

func fatal(w io.Writer, jout bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	switch jout {
	case true:
		fmt.Fprintf(w, fatalString, msg)
	default:
		fmt.Fprint(w, msg)
	}

	log.Fatal(msg)
}
