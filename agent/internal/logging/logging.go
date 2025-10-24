package logging

import (
	"log"
	"os"
)

func New() *log.Logger {
	return log.New(os.Stdout, "pingsanto-agent ", log.LstdFlags|log.LUTC)
}
