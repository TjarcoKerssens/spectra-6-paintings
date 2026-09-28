package e6

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenPath holds the ink mix the pipeline produces for testdata/sample.png.
//
// This is the net under the whole conversion. Every earlier test checks one stage
// in isolation, and a stage can be individually defensible and still combine into
// a bad picture: the first Go version of the error diffusion passed every unit
// test while turning photographs into flat blobs of yellow and red, because it
// accumulated error in linear light instead of sRGB. What that looked like in
// numbers was the ink mix swinging by tens of percent.
//
// So the check is the share of each ink. It is stable across platforms in a way an
// exact pixel hash is not, and it moves loudly when the dither changes character.
// Update it with -update, but only after looking at the output.
const goldenPath = "testdata/ink_shares.json"

var update = os.Getenv("UPDATE_GOLDEN") != ""

func inkShares(indices []uint8) map[string]float64 {
	counts := make(map[string]float64, len(InkNames))
	for _, name := range InkNames {
		counts[name] = 0
	}
	for _, v := range indices {
		counts[InkNames[v]]++
	}
	for k := range counts {
		counts[k] = counts[k] / float64(len(indices)) * 100
	}
	return counts
}

func TestGoldenInkMix(t *testing.T) {
	prm := Defaults()
	prm.Width, prm.Height = 300, 400
	res, _, err := Run("../../testdata/sample.png", Measured, prm)
	if err != nil {
		t.Fatal(err)
	}
	got := inkShares(res.Indices)

	path := filepath.Join("../..", goldenPath)
	if update {
		raw, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated:", got)
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with UPDATE_GOLDEN=1 go test ./...)", err)
	}
	var want map[string]float64
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for _, name := range InkNames {
		// A point and a half of drift absorbs resampling noise. The failure this
		// guards against moves shares by ten points or more.
		if diff := got[name] - want[name]; diff > 1.5 || diff < -1.5 {
			t.Errorf("%s ink share is %.1f%%, golden says %.1f%%; "+
				"look at the output before updating the golden", name, got[name], want[name])
		}
	}
}
