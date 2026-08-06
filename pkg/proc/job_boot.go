package proc

import (
	"context"

	log "github.com/sirupsen/logrus"
)

func (job *BootJob) Run(ctx context.Context) error {
	err := job.startOnce(ctx, nil)
	if err != nil && job.Config.CanFail {
		log.WithField("job.name", job.Config.Name).
			WithError(err).
			Warn("job failed, but is allowed to fail")
		return nil
	}
	return err
}
