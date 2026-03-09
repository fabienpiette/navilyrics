package tagger

import (
	"fmt"
	"strings"

	id3 "github.com/bogem/id3v2/v2"
)

type mp3Tagger struct{}

func (t *mp3Tagger) ReadLyrics(path string) (plain, synced string, err error) {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return "", "", fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	// USLT frame holds plain (unsynced) lyrics
	for _, f := range tag.GetFrames(tag.CommonID("Unsynchronised lyrics/text transcription")) {
		if ulf, ok := f.(id3.UnsynchronisedLyricsFrame); ok {
			plain = ulf.Lyrics
			break
		}
	}

	// TXXX with description "SYNCEDLYRICS" holds the LRC string
	for _, f := range tag.GetFrames("TXXX") {
		if tf, ok := f.(id3.UserDefinedTextFrame); ok {
			if strings.EqualFold(tf.Description, "SYNCEDLYRICS") {
				synced = tf.Value
				break
			}
		}
	}
	return plain, synced, nil
}

func (t *mp3Tagger) WriteLyrics(path, plain, synced string) error {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	if plain != "" {
		tag.DeleteFrames(tag.CommonID("Unsynchronised lyrics/text transcription"))
		tag.AddUnsynchronisedLyricsFrame(id3.UnsynchronisedLyricsFrame{
			Encoding: id3.EncodingUTF8,
			Language: "eng",
			Lyrics:   plain,
		})
	}

	if synced != "" {
		// Preserve existing TXXX frames that are NOT SYNCEDLYRICS
		existing := tag.GetFrames("TXXX")
		tag.DeleteFrames("TXXX")
		for _, f := range existing {
			if tf, ok := f.(id3.UserDefinedTextFrame); ok {
				if !strings.EqualFold(tf.Description, "SYNCEDLYRICS") {
					tag.AddFrame("TXXX", tf)
				}
			}
		}
		tag.AddFrame("TXXX", id3.UserDefinedTextFrame{
			Encoding:    id3.EncodingUTF8,
			Description: "SYNCEDLYRICS",
			Value:       synced,
		})
	}

	if err := tag.Save(); err != nil {
		return fmt.Errorf("mp3 save %s: %w", path, err)
	}
	return nil
}
