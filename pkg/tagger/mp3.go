package tagger

import (
	"bytes"
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
		// TXXX:SYNCEDLYRICS — stores the raw LRC string for external players.
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

		// SYLT — standard synced lyrics frame, read by Navidrome and most players.
		if body := encodeSYLT(synced); body != nil {
			tag.DeleteFrames("SYLT")
			tag.AddFrame("SYLT", id3.UnknownFrame{Body: body})
		}
	}

	if err := tag.Save(); err != nil {
		return fmt.Errorf("mp3 save %s: %w", path, err)
	}
	return nil
}

// BackfillSYLT adds a SYLT frame to an MP3 that already has TXXX:SYNCEDLYRICS
// but no SYLT. Returns true if the file was modified, false if it was skipped
// (already has SYLT, or has no TXXX:SYNCEDLYRICS to read from).
func BackfillSYLT(path string) (bool, error) {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return false, fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	if len(tag.GetFrames("SYLT")) > 0 {
		return false, nil // already has SYLT
	}

	var synced string
	for _, f := range tag.GetFrames("TXXX") {
		if tf, ok := f.(id3.UserDefinedTextFrame); ok {
			if strings.EqualFold(tf.Description, "SYNCEDLYRICS") {
				synced = tf.Value
				break
			}
		}
	}
	if synced == "" {
		return false, nil // nothing to backfill
	}

	body := encodeSYLT(synced)
	if body == nil {
		return false, nil
	}
	tag.AddFrame("SYLT", id3.UnknownFrame{Body: body})
	if err := tag.Save(); err != nil {
		return false, fmt.Errorf("mp3 save %s: %w", path, err)
	}
	return true, nil
}

// encodeSYLT builds the raw ID3v2 SYLT frame body from an LRC string.
//
// Frame layout (ID3v2 spec):
//
//	encoding(1) + language(3) + timestamp_format(1) + content_type(1)
//	+ content_descriptor(null-terminated) + (text + 0x00 + uint32_ms)*
//
// Uses UTF-8 encoding (0x03), millisecond timestamps (0x02), lyrics content type (0x01).
func encodeSYLT(lrc string) []byte {
	lines := parseLRC(lrc)
	if len(lines) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.WriteByte(0x03)    // encoding: UTF-8
	b.WriteString("eng") // language
	b.WriteByte(0x02)    // timestamp format: milliseconds
	b.WriteByte(0x01)    // content type: lyrics
	b.WriteByte(0x00)    // content descriptor: empty, null-terminated
	for _, line := range lines {
		b.WriteString(line.text)
		b.WriteByte(0x00) // text null terminator
		ts := line.ms
		b.WriteByte(byte(ts >> 24))
		b.WriteByte(byte(ts >> 16))
		b.WriteByte(byte(ts >> 8))
		b.WriteByte(byte(ts))
	}
	return b.Bytes()
}
