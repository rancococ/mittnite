package main

import (
	"time"

	log "github.com/sirupsen/logrus"
)

func init() {
	Formatter := new(log.TextFormatter)
	Formatter.TimestampFormat = time.RFC3339
	Formatter.FullTimestamp = true
	log.SetFormatter(Formatter)
}

func main() {
	Execute()
}
