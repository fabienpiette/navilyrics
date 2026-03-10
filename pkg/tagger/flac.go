package tagger

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
)

type flacTagger struct{}

func (t *flacTagger) ReadLyrics(path string) (plain, synced string, err error) {
	f, err := flac.ParseFile(path)
	if err != nil {
		return "", "", fmt.Errorf("flac parse %s: %w", path, err)
	}
	defer f.Close()

	for _, block := range f.Blocks {
		vc, ok := block.Body.(*meta.VorbisComment)
		if !ok {
			continue
		}
		for _, tag := range vc.Tags {
			switch strings.ToUpper(tag[0]) {
			case "LYRICS":
				plain = tag[1]
			case "SYNCEDLYRICS":
				synced = tag[1]
			}
		}
	}
	return plain, synced, nil
}

func (t *flacTagger) WriteLyrics(path, plain, synced string) error {
	src, err := flac.Open(path)
	if err != nil {
		return fmt.Errorf("flac open %s: %w", path, err)
	}
	defer src.Close()

	// Find or create VorbisComment block.
	var vc *meta.VorbisComment
	for _, block := range src.Blocks {
		if v, ok := block.Body.(*meta.VorbisComment); ok {
			vc = v
			break
		}
	}
	var vcBlock *meta.Block
	if vc == nil {
		vc = &meta.VorbisComment{Vendor: "navilyrics"}
		vcBlock = &meta.Block{
			Header: meta.Header{Type: meta.TypeVorbisComment},
			Body:   vc,
		}
		src.Blocks = append(src.Blocks, vcBlock)
	} else {
		for _, b := range src.Blocks {
			if _, ok := b.Body.(*meta.VorbisComment); ok {
				vcBlock = b
				break
			}
		}
	}

	// Remove existing LYRICS / SYNCEDLYRICS tags, then add new ones.
	filtered := vc.Tags[:0]
	for _, tag := range vc.Tags {
		upper := strings.ToUpper(tag[0])
		if upper != "LYRICS" && upper != "SYNCEDLYRICS" {
			filtered = append(filtered, tag)
		}
	}
	if plain != "" {
		filtered = append(filtered, [2]string{"LYRICS", plain})
	}
	if synced != "" {
		filtered = append(filtered, [2]string{"SYNCEDLYRICS", synced})
	}
	vc.Tags = filtered

	// encodeBlock guards: if block.Length == 0 it writes an empty block and
	// skips the body entirely. encodeVorbisComment recomputes the real length,
	// so we just need any non-zero placeholder to get past the guard.
	vcBlock.Header.Length = vorbisCommentLength(vc)

	// Atomic write: encode to temp file, then rename.
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("flac create tmp: %w", err)
	}

	enc, err := flac.NewEncoder(out, src.Info, src.Blocks...)
	if err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("flac new encoder: %w", err)
	}

	// Copy all audio frames.
	for {
		f, err := src.ParseNext()
		if err != nil {
			if err == io.EOF {
				break
			}
			enc.Close()
			out.Close()
			os.Remove(tmp)
			return fmt.Errorf("flac parse frame: %w", err)
		}
		if err := enc.WriteFrame(f); err != nil {
			enc.Close()
			out.Close()
			os.Remove(tmp)
			return fmt.Errorf("flac write frame: %w", err)
		}
	}

	// enc.Close() flushes and closes the underlying writer.
	if err := enc.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("flac close encoder: %w", err)
	}
	return os.Rename(tmp, path)
}

// vorbisCommentLength returns the byte length of a VorbisComment block body,
// matching the formula used by mewkiz/flac's encodeVorbisComment.
func vorbisCommentLength(vc *meta.VorbisComment) int64 {
	nbits := int64(32+8*len(vc.Vendor)) + 32
	for _, tag := range vc.Tags {
		nbits += int64(32 + 8*(len(tag[0])+1+len(tag[1])))
	}
	return nbits / 8
}
