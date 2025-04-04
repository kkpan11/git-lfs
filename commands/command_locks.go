package commands

import (
	"io"
	"os"
	"sort"
	"strings"

	"github.com/git-lfs/git-lfs/v3/errors"
	"github.com/git-lfs/git-lfs/v3/git"
	"github.com/git-lfs/git-lfs/v3/locking"
	"github.com/git-lfs/git-lfs/v3/tr"
	"github.com/spf13/cobra"
)

var (
	locksCmdFlags = new(locksFlags)
)

func locksCommand(cmd *cobra.Command, args []string) {
	lockData, err := computeLockData()
	if err != nil {
		ExitWithError(err)
	}
	filters, err := locksCmdFlags.Filters(lockData)
	if err != nil {
		Exit(tr.Tr.Get("Error building filters: %v", err))
	}

	if len(lockRemote) > 0 {
		cfg.SetRemote(lockRemote)
	}

	refUpdate := git.NewRefUpdate(cfg.Git, cfg.PushRemote(), cfg.CurrentRef(), nil)
	lockClient := newLockClient()
	lockClient.RemoteRef = refUpdate.RemoteRef()
	defer lockClient.Close()

	if locksCmdFlags.Cached {
		if locksCmdFlags.Limit > 0 {
			Exit(tr.Tr.Get("--cached option can't be combined with --limit"))
		}
		if len(filters) > 0 {
			Exit(tr.Tr.Get("--cached option can't be combined with filters"))
		}
		if locksCmdFlags.Local {
			Exit(tr.Tr.Get("--cached option can't be combined with --local"))
		}
	}

	if locksCmdFlags.Verify {
		if len(filters) > 0 {
			Exit(tr.Tr.Get("--verify option can't be combined with filters"))
		}
		if locksCmdFlags.Local {
			Exit(tr.Tr.Get("--verify option can't be combined with --local"))
		}
	}

	var locks []locking.Lock
	var locksOwned map[locking.Lock]bool
	var jsonWriteFunc func(io.Writer) error
	if locksCmdFlags.Verify {
		var ourLocks, theirLocks []locking.Lock
		ourLocks, theirLocks, err = lockClient.SearchLocksVerifiable(locksCmdFlags.Limit, locksCmdFlags.Cached)
		jsonWriteFunc = func(writer io.Writer) error {
			return lockClient.EncodeLocksVerifiable(ourLocks, theirLocks, writer)
		}

		locks = append(ourLocks, theirLocks...)
		locksOwned = make(map[locking.Lock]bool)
		for _, lock := range ourLocks {
			locksOwned[lock] = true
		}
	} else {
		locks, err = lockClient.SearchLocks(filters, locksCmdFlags.Limit, locksCmdFlags.Local, locksCmdFlags.Cached)
		jsonWriteFunc = func(writer io.Writer) error {
			return lockClient.EncodeLocks(locks, writer)
		}
	}

	// Print any we got before exiting

	if locksCmdFlags.JSON {
		if err := jsonWriteFunc(os.Stdout); err != nil {
			Error(err.Error())
		}
		return
	}

	var maxPathLen int
	var maxNameLen int
	lockPaths := make([]string, 0, len(locks))
	locksByPath := make(map[string]locking.Lock)
	for _, lock := range locks {
		lockPaths = append(lockPaths, lock.Path)
		locksByPath[lock.Path] = lock
		maxPathLen = max(maxPathLen, len(lock.Path))
		if lock.Owner != nil {
			maxNameLen = max(maxNameLen, len(lock.Owner.Name))
		}
	}

	sort.Strings(lockPaths)
	for _, lockPath := range lockPaths {
		var ownerName string
		lock := locksByPath[lockPath]
		if lock.Owner != nil {
			ownerName = lock.Owner.Name
		}

		pathPadding := max(maxPathLen-len(lock.Path), 0)
		namePadding := max(maxNameLen-len(ownerName), 0)
		kind := ""
		if locksOwned != nil {
			if locksOwned[lock] {
				kind = "O "
			} else {
				kind = "  "
			}
		}

		Print("%s%s%s\t%s%s\tID:%s", kind, lock.Path, strings.Repeat(" ", pathPadding),
			ownerName, strings.Repeat(" ", namePadding),
			lock.Id,
		)
	}

	if err != nil {
		Exit(tr.Tr.Get("Error while retrieving locks: %v", errors.Cause(err)))
	}
}

// locksFlags wraps up and holds all of the flags that can be given to the
// `git lfs locks` command.
type locksFlags struct {
	// Path is an optional filter parameter to filter against the lock's
	// path
	Path string
	// Id is an optional filter parameter used to filtere against the lock's
	// ID.
	Id string
	// limit is an optional request parameter sent to the server used to
	// limit the
	Limit int
	// local limits the scope of lock reporting to the locally cached record
	// of locks for the current user & doesn't query the server
	Local bool
	// JSON is an optional parameter to output data in json format.
	JSON bool
	// for non-local queries, report cached query results from the last query
	// instead of actually querying the server again
	Cached bool
	// for non-local queries, verify lock owner on server and
	// denote our locks in output
	Verify bool
}

// Filters produces a filter based on locksFlags instance.
func (l *locksFlags) Filters(data *lockData) (map[string]string, error) {
	filters := make(map[string]string)

	if l.Path != "" {
		path, err := lockPath(data, l.Path)
		if err != nil {
			return nil, err
		}

		filters["path"] = path
	}
	if l.Id != "" {
		filters["id"] = l.Id
	}

	return filters, nil
}

func init() {
	RegisterCommand("locks", locksCommand, func(cmd *cobra.Command) {
		cmd.Flags().StringVarP(&lockRemote, "remote", "r", "", "specify which remote to use when interacting with locks")
		cmd.Flags().StringVarP(&locksCmdFlags.Path, "path", "p", "", "filter locks results matching a particular path")
		cmd.Flags().StringVarP(&locksCmdFlags.Id, "id", "i", "", "filter locks results matching a particular ID")
		cmd.Flags().IntVarP(&locksCmdFlags.Limit, "limit", "l", 0, "optional limit for number of results to return")
		cmd.Flags().BoolVarP(&locksCmdFlags.Local, "local", "", false, "only list cached local record of own locks")
		cmd.Flags().BoolVarP(&locksCmdFlags.Cached, "cached", "", false, "list cached lock information from the last remote query, instead of actually querying the server")
		cmd.Flags().BoolVarP(&locksCmdFlags.Verify, "verify", "", false, "verify lock owner on server and mark own locks by 'O'")
		cmd.Flags().BoolVarP(&locksCmdFlags.JSON, "json", "j", false, "Give the output in a stable JSON format for scripts")
	})
}
