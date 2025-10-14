package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

var LogTag = setLogTag()

const defaultLogTag = "readmsgs"

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

func fatal(w io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	fmt.Fprintf(w, fatalString, msg)
	log.Fatal(msg)
}
