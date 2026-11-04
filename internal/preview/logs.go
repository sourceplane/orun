package preview

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// CopyLog writes a component's log to w. With follow it keeps writing what
// is appended until ctx ends, starting over when a restart truncates it.
func CopyLog(ctx context.Context, w io.Writer, root, component string, follow bool) error {
	path := LogPath(root, component)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !follow {
			return fmt.Errorf("no log for %s at %s", component, path)
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	var off int64
	if f != nil {
		n, err := io.Copy(w, f)
		f.Close()
		if err != nil {
			return err
		}
		off = n
	}
	if !follow {
		return nil
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() < off {
			off = 0 // a restart began a new log
		}
		if info.Size() == off {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(off, io.SeekStart); err == nil {
			n, cErr := io.Copy(w, f)
			off += n
			if cErr != nil {
				f.Close()
				return cErr
			}
		}
		f.Close()
	}
}
