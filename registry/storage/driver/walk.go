package driver

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/sirupsen/logrus"
)

// ErrSkipDir is used as a return value from onFileFunc to indicate that
// the directory named in the call is to be skipped. It is not returned
// as an error by any function.
var ErrSkipDir = errors.New("skip this directory")

// WalkFn is called once per file by Walk
type WalkFn func(fileInfo FileInfo) error

// WalkFallback traverses a filesystem defined within driver, starting
// from the given path, calling f on each file. It uses the List method and Stat to drive itself.
// If the returned error from the WalkFn is ErrSkipDir and fileInfo refers
// to a directory, the directory will not be entered and Walk
// will continue the traversal.  If fileInfo refers to a normal file, processing stops
func WalkFallback(ctx context.Context, driver StorageDriver, from string, f WalkFn) error {
	children, err := driver.List(ctx, from)
	if err != nil {
		return err
	}
	sort.Stable(sort.StringSlice(children))
	for _, child := range children {
		// TODO(stevvooe): Calling driver.Stat for every entry is quite
		// expensive when running against backends with a slow Stat
		// implementation, such as s3. This is very likely a serious
		// performance bottleneck.
		fileInfo, err := driver.Stat(ctx, child)
		if err != nil {
			switch err.(type) {
			case PathNotFoundError:
				// repository was removed in between listing and enumeration. Ignore it.
				logrus.WithField("path", child).Infof("ignoring deleted path")
				continue
			default:
				return err
			}
		}
		err = f(fileInfo)
		if err == nil && fileInfo.IsDir() {
			if err := WalkFallback(ctx, driver, child, f); err != nil {
				return err
			}
		} else if err == ErrSkipDir {
			// Stop iteration if it's a file, otherwise noop if it's a directory
			if !fileInfo.IsDir() {
				return nil
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

// WalkFallbackParallel is similar to WalkFalkback, but processes files and
// directories in their own goroutines
func WalkFallbackParallel(ctx context.Context, driver StorageDriver, from string, f WalkFn) error {
	var retError error
	errors := make(chan error)
	quit := make(chan struct{})

	// If we encounter an error from any goroutine called from within doWalk,
	// return early from any new goroutines and return that error.
	go func() {
		for err := range errors {
			retError = err
			close(quit)
		}
	}()

	// doWalk spawns and manages it's own goroutines, but it also calls
	// itself recursively. Passing in a WaitGroup allows us to wait for the
	// entire walk to complete without blocking on each doWalk call.
	var wg sync.WaitGroup

	doWalkParallel(ctx, driver, &wg, quit, errors, from, f)

	wg.Wait()

	return retError
}

func doWalkParallel(ctx context.Context, driver StorageDriver, wg *sync.WaitGroup, quit <-chan struct{}, errors chan<- error, from string, f WalkFn) {
	select {
	// The walk was canceled, return to stop requests for pages and prevent gorountines from leaking.
	case <-quit:
		return
	default:
		children, err := driver.List(ctx, from)
		if err != nil {
			errors <- err
			return
		}
		for _, child := range children {
			wg.Add(1)
			c := child
			go func() {
				defer wg.Done()
				// TODO(stevvooe): Calling driver.Stat for every entry is quite
				// expensive when running against backends with a slow Stat
				// implementation, such as s3. This is very likely a serious
				// performance bottleneck.
				fileInfo, err := driver.Stat(ctx, c)
				if err != nil {
					switch err.(type) {
					case PathNotFoundError:
						// repository was removed in between listing and enumeration. Ignore it.
						logrus.WithField("path", c).Infof("ignoring deleted path")
						return
					default:
						errors <- err
						return
					}
				}
				err = f(fileInfo)
				if err == nil && fileInfo.IsDir() {
					if err := WalkFallback(ctx, driver, c, f); err != nil {
						errors <- err
						return
					}
				} else if err == ErrSkipDir {
					// Stop iteration if it's a file, otherwise noop if it's a directory
					if !fileInfo.IsDir() {
						return
					}
				} else if err != nil {
					errors <- err
					return
				}
				return
			}()
		}
	}
}
