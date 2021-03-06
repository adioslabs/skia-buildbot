package paramsets

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.skia.org/infra/go/tiling"
	"go.skia.org/infra/golden/go/tally"
	"go.skia.org/infra/golden/go/types"
)

func TestParamset(t *testing.T) {
	tile := &tiling.Tile{
		Traces: map[string]tiling.Trace{
			"a": &types.GoldenTrace{
				Values: []string{"aaa", "bbb"},
				Params_: map[string]string{
					"name":             "foo",
					"config":           "8888",
					types.CORPUS_FIELD: "gm"},
			},
			"b": &types.GoldenTrace{
				Values: []string{"ccc", "ddd", "aaa"},
				Params_: map[string]string{
					"name":             "foo",
					"config":           "565",
					types.CORPUS_FIELD: "gm"},
			},
			"c": &types.GoldenTrace{
				Values: []string{"eee", types.MISSING_DIGEST},
				Params_: map[string]string{
					"name":             "foo",
					"config":           "gpu",
					types.CORPUS_FIELD: "gm"},
			},
		},
	}

	tallies := map[string]tally.Tally{
		"a": tally.Tally{
			"aaa": 1,
			"bbb": 1,
		},
		"b": tally.Tally{
			"ccc": 1,
			"ddd": 1,
			"aaa": 1,
		},
		"unknown": tally.Tally{
			"ccc": 1,
			"ddd": 1,
			"aaa": 1,
		},
	}

	byTrace := byTraceForTile(tile, tallies)

	// Test that we are robust to traces appearing in tallies, but not in the tile, and vice-versa.
	assert.Equal(t, byTrace["foo:bbb"]["config"], []string{"8888"})

	assert.Equal(t, byTrace["foo:aaa"]["name"], []string{"foo"})
	got := byTrace["foo:aaa"]["config"]
	sort.Strings(got)
	assert.Equal(t, got, []string{"565", "8888"})

	assert.Nil(t, byTrace["bar:fff"])
}
