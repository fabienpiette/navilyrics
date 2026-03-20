package lyrics

import (
	"context"
	"time"

	"github.com/user/navilyrics/pkg/goscribe"
)

type Transcriber interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

type goscribeTranscriber struct {
	c    *goscribe.Client
	song bool
}

// NewGoscribeTranscriber creates a Transcriber backed by goscribe.
// Set song=true to enable vocal extraction and lyrics validation (requires
// demucs on the goscribe server).
func NewGoscribeTranscriber(c *goscribe.Client, song bool) Transcriber {
	return &goscribeTranscriber{c: c, song: song}
}

func (t *goscribeTranscriber) Name() string { return "goscribe" }

func (t *goscribeTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	jobID, err := t.c.SubmitJob(ctx, audioPath, goscribe.JobOptions{Song: t.song})
	if err != nil {
		return "", err
	}
	return t.c.PollJob(ctx, jobID, 3*time.Second)
}
