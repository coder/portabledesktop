package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newCacheCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the portabledesktop cache",
	}

	cmd.AddCommand(newCacheCleanCommand(stdout, stderr))
	return cmd
}

func newCacheCleanCommand(stdout, stderr io.Writer) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove cached runtime directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cacheHome := os.Getenv("XDG_CACHE_HOME")
			if cacheHome == "" {
				home := os.Getenv("HOME")
				cacheHome = filepath.Join(home, ".cache")
			}
			cacheDir := filepath.Join(cacheHome, "portabledesktop")

			entries, err := os.ReadDir(cacheDir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(stdout, "no cache directory found")
					return nil
				}
				return err
			}

			var (
				count     int
				totalSize int64
			)

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				if !strings.HasPrefix(entry.Name(), "runtime-") {
					continue
				}

				dirPath := filepath.Join(cacheDir, entry.Name())
				size, sizeErr := dirSize(dirPath)
				if sizeErr != nil {
					size = 0
				}

				if dryRun {
					fmt.Fprintf(stdout,
						"would remove: %s (%s)\n",
						dirPath, formatBytes(size),
					)
				} else {
					if err := os.RemoveAll(dirPath); err != nil {
						fmt.Fprintf(stderr,
							"failed to remove %s: %v\n",
							dirPath, err,
						)
						continue
					}
				}

				count++
				totalSize += size
			}

			if dryRun {
				fmt.Fprintf(stdout,
					"would remove %d directories (%s total)\n",
					count, formatBytes(totalSize),
				)
			} else {
				fmt.Fprintf(stdout,
					"removed %d directories (%s freed)\n",
					count, formatBytes(totalSize),
				)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list directories without deleting")
	return cmd
}

// dirSize walks a directory tree and sums the sizes of all regular
// files.
func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// formatBytes returns a human-readable byte count.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
