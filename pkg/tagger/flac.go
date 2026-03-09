// pkg/tagger/flac.go — placeholder until Task 5
package tagger

type flacTagger struct{}

func (t *flacTagger) ReadLyrics(path string) (string, string, error) {
	return "", "", nil
}

func (t *flacTagger) WriteLyrics(path, plain, synced string) error {
	return nil
}
