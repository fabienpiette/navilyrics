package tagger

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"

	id3 "github.com/bogem/id3v2/v2"
)

// IsBodyOverflow reports whether err is the malformed ID3v2 frame-size error
// produced by bogem/id3v2 when a frame's declared size exceeds the tag area.
// This typically means the file was written with synchsafe frame sizes by a
// tagger that declared an ID3v2.3 header (or vice versa).
func IsBodyOverflow(err error) bool {
	return errors.Is(err, id3.ErrBodyOverflow)
}

// RepairID3 attempts to fix malformed ID3v2 tag structures in path.
// It runs two repair passes in sequence:
//  1. mp3val (if available) — fixes MPEG audio frame integrity issues.
//  2. fixSynchsafeFrames — corrects frame sizes written as synchsafe integers
//     in an ID3v2.3 tag (a common tagger bug that mp3val does not address).
//
// If mp3val is not installed, only the Go-native synchsafe fix is attempted.
// The -nb flag suppresses mp3val backup file creation.
func RepairID3(path string) error {
	if mp3valBin, err := exec.LookPath("mp3val"); err == nil {
		out, err := exec.Command(mp3valBin, "-f", "-nb", path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("mp3val: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	// Second pass: fix synchsafe frame-size mismatch (soft — may not apply to all files).
	if err := fixSynchsafeFrames(path); err != nil {
		log.Printf("tags: synchsafe repair skipped %s: %v", path, err)
	}
	return nil
}

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

func (t *mp3Tagger) IsInstrumental(path string) (bool, error) {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return false, fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	for _, f := range tag.GetFrames("TXXX") {
		if tf, ok := f.(id3.UserDefinedTextFrame); ok {
			if strings.EqualFold(tf.Description, "NAVILYRICS_INSTRUMENTAL") {
				return tf.Value == "1", nil
			}
		}
	}
	return false, nil
}

func (t *mp3Tagger) MarkInstrumental(path string) error {
	tag, err := id3.Open(path, id3.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	existing := tag.GetFrames("TXXX")
	tag.DeleteFrames("TXXX")
	for _, f := range existing {
		if tf, ok := f.(id3.UserDefinedTextFrame); ok {
			if !strings.EqualFold(tf.Description, "NAVILYRICS_INSTRUMENTAL") {
				tag.AddFrame("TXXX", tf)
			}
		}
	}
	tag.AddFrame("TXXX", id3.UserDefinedTextFrame{
		Encoding:    id3.EncodingUTF8,
		Description: "NAVILYRICS_INSTRUMENTAL",
		Value:       "1",
	})

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
