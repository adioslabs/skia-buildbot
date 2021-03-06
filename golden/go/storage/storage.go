package storage

import (
	"fmt"
	"sync"
	"time"

	"github.com/skia-dev/glog"
	"go.skia.org/infra/go/eventbus"
	"go.skia.org/infra/go/rietveld"
	"go.skia.org/infra/go/tiling"
	tracedb "go.skia.org/infra/go/trace/db"
	"go.skia.org/infra/golden/go/diff"
	"go.skia.org/infra/golden/go/digeststore"
	"go.skia.org/infra/golden/go/expstorage"
	"go.skia.org/infra/golden/go/ignore"
	"go.skia.org/infra/golden/go/trybot"
	"go.skia.org/infra/golden/go/types"
)

// Storage is a container struct for the various storage objects we are using.
// It is intended to reduce parameter lists as we pass around storage objects.
type Storage struct {
	DiffStore         diff.DiffStore
	ExpectationsStore expstorage.ExpectationsStore
	IgnoreStore       ignore.IgnoreStore
	MasterTileBuilder tracedb.MasterTileBuilder
	BranchTileBuilder tracedb.BranchTileBuilder
	DigestStore       digeststore.DigestStore
	EventBus          *eventbus.EventBus
	TrybotResults     *trybot.TrybotResults
	RietveldAPI       *rietveld.Rietveld

	// NCommits is the number of commits we should consider. If NCommits is
	// 0 or smaller all commits in the last tile will be considered.
	NCommits int

	// Internal variables used to cache trimmed tiles.
	lastTrimmedTile        *tiling.Tile
	lastTrimmedIgnoredTile *tiling.Tile
	lastIgnoreRev          int64
	mutex                  sync.Mutex
}

// GetTileStreamNow is a utility function that reads tiles in the given
// interval and sends them on the returned channel.
// The first tile is send immediately.
// Should the call to read a new tile fail it will send that last
// successfully read tile. Thus it guarantees to send a tile in the provided
// interval, assuming at least one tile could be read.
func (s *Storage) GetTileStreamNow(interval time.Duration) <-chan *types.TilePair {
	retCh := make(chan *types.TilePair)

	go func() {
		var lastTile *types.TilePair = nil

		readOneTile := func() {
			if tilePair, err := s.GetLastTileTrimmed(); err != nil {
				// Log the error and send the best tile we have right now.
				glog.Errorf("Error reading tile: %s", err)
				if lastTile != nil {
					retCh <- lastTile
				}
			} else {
				lastTile = tilePair
				retCh <- tilePair
			}
		}

		readOneTile()
		for _ = range time.Tick(interval) {
			readOneTile()
		}
	}()

	return retCh
}

// DrainChangeChannel removes everything from the channel thats currently
// buffered or ready to be read.
func DrainChangeChannel(ch <-chan []string) {
Loop:
	for {
		select {
		case <-ch:
		default:
			break Loop
		}
	}
}

// GetLastTrimmed returns the last tile as read-only trimmed to contain at
// most NCommits. It caches trimmed tiles as long as the underlying tiles
// do not change.
func (s *Storage) GetLastTileTrimmed() (*types.TilePair, error) {
	// Retieve the most recent tile.
	tile := s.MasterTileBuilder.GetTile()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.NCommits <= 0 {
		return &types.TilePair{
			Tile:            tile,
			TileWithIgnores: tile,
		}, nil
	}

	currentIgnoreRev := s.IgnoreStore.Revision()

	// Check if the tile hasn't changed and the ignores haven't changed.
	if s.lastTrimmedTile != nil && tile == s.lastTrimmedTile && s.lastTrimmedIgnoredTile != nil && currentIgnoreRev == s.lastIgnoreRev {
		return &types.TilePair{
			Tile:            s.lastTrimmedIgnoredTile,
			TileWithIgnores: s.lastTrimmedTile,
		}, nil
	}

	// Get the tile without the ignored traces.
	retIgnoredTile, err := FilterIgnored(tile, s.IgnoreStore)
	if err != nil {
		return nil, err
	}

	// Cache this tile.
	s.lastIgnoreRev = currentIgnoreRev
	s.lastTrimmedTile = tile
	s.lastTrimmedIgnoredTile = retIgnoredTile

	return &types.TilePair{
		Tile:            s.lastTrimmedIgnoredTile,
		TileWithIgnores: s.lastTrimmedTile,
	}, nil
}

// FilterIgnored returns a copy of the given tile with all traces removed
// that match the ignore rules in the given ignore store.
func FilterIgnored(inputTile *tiling.Tile, ignoreStore ignore.IgnoreStore) (*tiling.Tile, error) {
	ignores, err := ignoreStore.List(false)
	if err != nil {
		return nil, fmt.Errorf("Failed to get ignores to filter tile: %s", err)
	}

	// Now copy the tile by value.
	ret := inputTile.Copy()

	// Then remove traces that should be ignored.
	ignoreQueries, err := ignore.ToQuery(ignores)
	if err != nil {
		return nil, err
	}
	for id, tr := range ret.Traces {
		for _, q := range ignoreQueries {
			if tiling.Matches(tr, q) {
				delete(ret.Traces, id)
				continue
			}
		}
	}
	return ret, nil
}

// GetOrUpdateDigestInfo is a helper function that retrieves the DigestInfo for
// the given test name/digest pair or updates the underlying info if it is not
// in the digest store yet.
func (s *Storage) GetOrUpdateDigestInfo(testName, digest string, commit *tiling.Commit) (*digeststore.DigestInfo, error) {
	digestInfo, ok, err := s.DigestStore.Get(testName, digest)
	if err != nil {
		return nil, err
	}

	if ok {
		return digestInfo, nil
	}
	digestInfo = &digeststore.DigestInfo{
		TestName: testName,
		Digest:   digest,
		First:    commit.CommitTime,
		Last:     commit.CommitTime,
	}
	err = s.DigestStore.Update([]*digeststore.DigestInfo{digestInfo})
	if err != nil {
		return nil, err
	}

	return digestInfo, nil
}

// GetTileFromTimeRange returns a tile that contains the commits in the given time range.
func (s *Storage) GetTileFromTimeRange(begin, end time.Time) (*tiling.Tile, error) {
	commitIDs, err := s.BranchTileBuilder.ListLong(begin, end, "master")
	if err != nil {
		return nil, fmt.Errorf("Failed retrieving commitIDs in range %s to %s. Got error: %s", begin, end, err)
	}
	return s.BranchTileBuilder.CachedTileFromCommits(tracedb.ShortFromLong(commitIDs))
}
