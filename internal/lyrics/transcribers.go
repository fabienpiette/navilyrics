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

type goscribeTranscriber struct{ c *goscribe.Client }

func NewGoscribeTranscriber(c *goscribe.Client) Transcriber {
	return &goscribeTranscriber{c: c}
}

func (t *goscribeTranscriber) Name() string { return "goscribe" }

func (t *goscribeTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	jobID, err := t.c.SubmitJob(ctx, audioPath)
	if err != nil {
		return "", err
	}
	return t.c.PollJob(ctx, jobID, 3*time.Second)
}
