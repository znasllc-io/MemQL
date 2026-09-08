// Command conceptsnapshot regenerates the committed per-concept field snapshot
// and, in --check mode, fails when the committed copy has gone stale
// (memql#5209).
//
// It is the SEVENTH regeneration gate, after memqllint, sdk-gen, arch-model,
// frontdoor-hosts, frontdoor-paths and proto-gen, and it has one behaviour none
// of those six has: it REFUSES TO WRITE when the tree has taken something away
// that the ledger does not record. The other six regenerate whatever they find;
// this one exists precisely to notice a removal, so absorbing one silently
// would be the same as not running.
//
//	make concept-snapshot          refresh the committed file
//	make concept-snapshot-check    CI gate: refuse a stale or unrecorded tree
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/znasllc-io/memql/component/conceptfields"
)

func main() {
	var (
		dslRoot    = flag.String("dsl", conceptfields.DefaultDSLRoot, "DSL tree to scan")
		snapshot   = flag.String("out", conceptfields.DefaultSnapshotPath, "snapshot file to read and write")
		migrations = flag.String("migrations", conceptfields.DefaultMigrationsDir, "up-migration directory the ledger is checked against")
		check      = flag.Bool("check", false, "verify the committed snapshot is current; write nothing")
	)
	flag.Parse()

	if err := run(*dslRoot, *snapshot, *migrations, *check); err != nil {
		fmt.Fprintf(os.Stderr, "conceptsnapshot: %v\n", err)
		os.Exit(1)
	}
}

func run(dslRoot, snapshotPath, migrationsDir string, check bool) error {
	result, err := conceptfields.Verify(dslRoot, snapshotPath, migrationsDir)
	if err != nil {
		return err
	}
	if problem := result.Problem(); problem != "" {
		return fmt.Errorf("%s", problem)
	}
	if check {
		if !bytes.Equal(result.Committed, result.Wanted) {
			return fmt.Errorf("%s is stale.\n\nRun `make concept-snapshot` and commit the result", snapshotPath)
		}
		fmt.Printf("concept snapshot current: %d concepts, %d recorded retirements\n",
			len(result.Snapshot.Concepts), len(result.Snapshot.Retired))
		return nil
	}
	if bytes.Equal(result.Committed, result.Wanted) {
		fmt.Printf("concept snapshot unchanged: %d concepts, %d recorded retirements\n",
			len(result.Snapshot.Concepts), len(result.Snapshot.Retired))
		return nil
	}
	if err := os.WriteFile(snapshotPath, result.Wanted, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d concepts, %d recorded retirements\n",
		snapshotPath, len(result.Snapshot.Concepts), len(result.Snapshot.Retired))
	return nil
}
