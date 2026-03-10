package tagger_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(err)
	}
	generateMP3Fixture()
	generateFLACFixture()
	os.Exit(m.Run())
}
