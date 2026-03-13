package tagger

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
)

// fixSynchsafeFrames corrects an ID3v2.3 tag whose frame sizes were written
// as synchsafe integers (an ID3v2.4 behaviour). bogem/id3v2 interprets v2.3
// frame sizes as plain 32-bit big-endian, so synchsafe-encoded sizes look much
// larger than the actual frames, triggering ErrBodyOverflow.
//
// The function re-reads every frame using synchsafe decoding; if all frames
// parse cleanly within the tag boundary, it rewrites them with the correct
// plain 32-bit sizes and saves the file atomically.
func fixSynchsafeFrames(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if len(data) < 10 || string(data[0:3]) != "ID3" {
		return fmt.Errorf("no ID3v2 header")
	}
	if data[3] != 3 {
		return fmt.Errorf("ID3v2.%d not supported for frame-size repair", data[3])
	}
	// Extended header flag (bit 6 of flags byte) complicates frame offsets — skip.
	if data[5]&0x40 != 0 {
		return fmt.Errorf("extended ID3v2 header not supported for frame-size repair")
	}

	tagSize := decodeSynchsafe4(data[6:10])
	end := 10 + int(tagSize)
	if end > len(data) {
		return fmt.Errorf("declared tag size exceeds file length")
	}

	corrected, err := reencodeFrameSizes(data[10:end])
	if err != nil {
		return err
	}

	// Rebuild: tag header (with updated size) + corrected frames + audio data.
	hdr := make([]byte, 10)
	copy(hdr, data[:6]) // preserve "ID3", version, flags
	encodeSynchsafe4(hdr[6:], uint32(len(corrected)))

	tmp := path + ".id3fix.tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	_, e1 := f.Write(hdr)
	_, e2 := f.Write(corrected)
	_, e3 := f.Write(data[end:]) // audio
	ce := f.Close()
	if e1 != nil || e2 != nil || e3 != nil || ce != nil {
		os.Remove(tmp)
		return fmt.Errorf("write tmp")
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	log.Printf("tags: corrected synchsafe frame sizes in %s", path)
	return nil
}

// reencodeFrameSizes walks raw ID3v2 frame bytes interpreting sizes as
// synchsafe integers and returns the same frames with sizes re-encoded as
// plain 32-bit big-endian (the correct ID3v2.3 format).
func reencodeFrameSizes(frames []byte) ([]byte, error) {
	var out []byte
	pos := 0
	for pos < len(frames) {
		if frames[pos] == 0x00 {
			break // padding
		}
		if pos+10 > len(frames) {
			return nil, fmt.Errorf("incomplete frame header at offset %d", pos)
		}
		id := frames[pos : pos+4]
		size := decodeSynchsafe4(frames[pos+4 : pos+8])
		flags := frames[pos+8 : pos+10]

		bodyEnd := pos + 10 + int(size)
		if bodyEnd > len(frames) {
			return nil, fmt.Errorf("frame %s: synchsafe size %d still overflows — not a synchsafe mismatch", id, size)
		}

		frame := make([]byte, 10+int(size))
		copy(frame[0:4], id)
		binary.BigEndian.PutUint32(frame[4:8], size) // plain 32-bit (v2.3 correct)
		copy(frame[8:10], flags)
		copy(frame[10:], frames[pos+10:bodyEnd])
		out = append(out, frame...)
		pos = bodyEnd
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no frames parsed with synchsafe interpretation")
	}
	return out, nil
}

func decodeSynchsafe4(b []byte) uint32 {
	return uint32(b[0])<<21 | uint32(b[1])<<14 | uint32(b[2])<<7 | uint32(b[3])
}

func encodeSynchsafe4(b []byte, v uint32) {
	b[0] = byte((v >> 21) & 0x7f)
	b[1] = byte((v >> 14) & 0x7f)
	b[2] = byte((v >> 7) & 0x7f)
	b[3] = byte(v & 0x7f)
}
