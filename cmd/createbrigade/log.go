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
