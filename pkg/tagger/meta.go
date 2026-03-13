package tagger

import (
	"fmt"
	"path/filepath"
	"strings"

	id3 "github.com/bogem/id3v2/v2"
)

// SongMeta holds editable song metadata fields.
type SongMeta struct {
	Title       string
	Artist      string
	Album       string
	Year        string
	TrackNumber string
	Genre       string
}

// WriteMeta writes SongMeta fields to the audio file's tags.
// For MP3: if force is true, existing frames are discarded and the ID3v2 tag
// is rebuilt from scratch — use this to recover files with corrupt frame data.
// For FLAC: force is ignored; tags are always updated in-place via Vorbis comments.
func WriteMeta(path string, m SongMeta, force bool) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return writeMetaMP3(path, m, force)
	case ".flac":
		return writeMetaFLAC(path, m)
	default:
		return fmt.Errorf("unsupported format for meta write: %q", ext)
	}
}

func writeMetaMP3(path string, m SongMeta, force bool) error {
	tag, err := id3.Open(path, id3.Options{Parse: !force})
	if err != nil {
		return fmt.Errorf("mp3 open %s: %w", path, err)
	}
	defer tag.Close()

	if m.Title != "" {
		tag.SetTitle(m.Title)
	}
	if m.Artist != "" {
		tag.SetArtist(m.Artist)
	}
	if m.Album != "" {
		tag.SetAlbum(m.Album)
	}
	if m.Year != "" {
		tag.SetYear(m.Year)
	}
	if m.TrackNumber != "" {
		tag.DeleteFrames("TRCK")
		tag.AddTextFrame("TRCK", id3.EncodingUTF8, m.TrackNumber)
	}
	if m.Genre != "" {
		tag.DeleteFrames("TCON")
		tag.AddTextFrame("TCON", id3.EncodingUTF8, m.Genre)
	}
	if err := tag.Save(); err != nil {
		return fmt.Errorf("mp3 save %s: %w", path, err)
	}
	return nil
}

func writeMetaFLAC(path string, m SongMeta) error {
	t := &flacTagger{}
	return t.updateVorbisComment(path, func(tags [][2]string) [][2]string {
		skip := map[string]bool{
			"TITLE": true, "ARTIST": true, "ALBUM": true,
			"DATE": true, "TRACKNUMBER": true, "GENRE": true,
		}
		out := tags[:0]
		for _, tag := range tags {
			if !skip[strings.ToUpper(tag[0])] {
				out = append(out, tag)
			}
		}
		if m.Title != "" {
			out = append(out, [2]string{"TITLE", m.Title})
		}
		if m.Artist != "" {
			out = append(out, [2]string{"ARTIST", m.Artist})
		}
		if m.Album != "" {
			out = append(out, [2]string{"ALBUM", m.Album})
		}
		if m.Year != "" {
			out = append(out, [2]string{"DATE", m.Year})
		}
		if m.TrackNumber != "" {
			out = append(out, [2]string{"TRACKNUMBER", m.TrackNumber})
		}
		if m.Genre != "" {
			out = append(out, [2]string{"GENRE", m.Genre})
		}
		return out
	})
}
