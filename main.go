package main

import (
	"os"
	"time"

	"github.com/mittwald/mittnite/cmd"
	log "github.com/sirupsen/logrus"
)

func init() {
	Formatter := new(log.TextFormatter)
	Formatter.TimestampFormat = time.RFC3339
	Formatter.FullTimestamp = true
	log.SetFormatter(Formatter)
	if os.Getenv("MITTNITE_LOG_LEVEL") == "debug" {
		log.SetLevel(log.DebugLevel)
	}
}

func main() {
	cmd.Execute()
}
