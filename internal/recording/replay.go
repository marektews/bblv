package recording

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"
)

// Replay reads a recording and calls fn for every entry (the meta entry
// included), waiting between reports for the recorded interval divided by
// speed. A speed of 0 replays as fast as possible.
func Replay(ctx context.Context, path string, speed float64, fn func(Entry)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReader(f))
	var prev time.Time
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		var e Entry
		if err = dec.Decode(&e); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if e.Kind == "report" {
			if !prev.IsZero() && speed > 0 {
				if d := time.Duration(float64(e.Time.Sub(prev)) / speed); d > 0 {
					t := time.NewTimer(d)
					select {
					case <-ctx.Done():
						t.Stop()
						return ctx.Err()
					case <-t.C:
					}
				}
			}
			prev = e.Time
		}
		fn(e)
	}
}
