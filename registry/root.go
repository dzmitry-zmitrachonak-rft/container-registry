package registry

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/migrations"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/storage"
	"github.com/docker/distribution/registry/storage/driver/factory"
	"github.com/docker/distribution/version"

	"github.com/docker/libtrust"
	"github.com/olekukonko/tablewriter"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func init() {
	RootCmd.AddCommand(ServeCmd)
	RootCmd.AddCommand(GCCmd)
	RootCmd.AddCommand(DBCmd)
	RootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "show the version and exit")

	GCCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "do everything except remove the blobs")
	GCCmd.Flags().BoolVarP(&removeUntagged, "delete-untagged", "m", false, "delete manifests that are not currently referenced via tag")
	GCCmd.Flags().StringVarP(&debugAddr, "debug-server", "s", "", "run a pprof debug server at <address:port>")

	MigrateCmd.AddCommand(MigrateVersionCmd)
	MigrateCmd.AddCommand(MigrateStatusCmd)
	MigrateUpCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "do not commit changes to the database")
	MigrateUpCmd.Flags().VarP(nullableInt{&maxNumMigrations}, "limit", "n", "limit the number of migrations (all by default)")
	MigrateUpCmd.Flags().BoolVarP(&skipPostDeployment, "skip-post-deployment", "s", false, "do not apply post deployment migrations")
	MigrateCmd.AddCommand(MigrateUpCmd)
	MigrateDownCmd.Flags().BoolVarP(&force, "force", "f", false, "no confirmation message")
	MigrateDownCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "do not commit changes to the database")
	MigrateDownCmd.Flags().VarP(nullableInt{&maxNumMigrations}, "limit", "n", "limit the number of migrations (all by default)")
	MigrateCmd.AddCommand(MigrateDownCmd)
	DBCmd.AddCommand(MigrateCmd)

	DBCmd.AddCommand(ImportCmd)
	ImportCmd.Flags().StringVarP(&repoPath, "repository", "r", "", "import a specific repository (all by default)")
	ImportCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "do not commit changes to the database")
	ImportCmd.Flags().BoolVarP(&importDanglingBlobs, "dangling-blobs", "b", false, "import all blobs, regardless of whether they are referenced by a manifest or not")
	ImportCmd.Flags().BoolVarP(&importDanglingManifests, "dangling-manifests", "m", false, "import all manifests, regardless of whether they are tagged or not")
	ImportCmd.Flags().BoolVarP(&continueImport, "continue-import", "c", false, "allow import to be retried on a database which is already partially imported")
}

var (
	continueImport          bool
	debugAddr               string
	dryRun                  bool
	force                   bool
	importDanglingBlobs     bool
	importDanglingManifests bool
	maxNumMigrations        *int
	removeUntagged          bool
	repoPath                string
	showVersion             bool
	skipPostDeployment      bool
)

// nullableInt implements spf13/pflag#Value as a custom nullable integer to capture spf13/cobra command flags.
// https://pkg.go.dev/github.com/spf13/pflag?tab=doc#Value
type nullableInt struct {
	ptr **int
}

func (f nullableInt) String() string {
	if *f.ptr == nil {
		return "0"
	}
	return strconv.Itoa(**f.ptr)
}

func (f nullableInt) Type() string {
	return "int"
}

func (f nullableInt) Set(s string) error {
	v, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f.ptr = &v
	return nil
}

// RootCmd is the main command for the 'registry' binary.
var RootCmd = &cobra.Command{
	Use:   "registry",
	Short: "`registry`",
	Long:  "`registry`",
	Run: func(cmd *cobra.Command, args []string) {
		if showVersion {
			version.PrintVersion()
			return
		}
		cmd.Usage()
	},
}

// GCCmd is the cobra command that corresponds to the garbage-collect subcommand
var GCCmd = &cobra.Command{
	Use:   "garbage-collect <config>",
	Short: "`garbage-collect` deletes layers not referenced by any manifests",
	Long:  "`garbage-collect` deletes layers not referenced by any manifests",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		driver, err := factory.Create(config.Storage.Type(), config.Storage.Parameters())
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct %s driver: %v", config.Storage.Type(), err)
			os.Exit(1)
		}

		ctx := dcontext.Background()
		ctx, err = configureLogging(ctx, config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to configure logging with config: %s", err)
			os.Exit(1)
		}

		k, err := libtrust.GenerateECP256PrivateKey()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		registry, err := storage.NewRegistry(ctx, driver, storage.Schema1SigningKey(k))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct registry: %v", err)
			os.Exit(1)
		}

		if debugAddr != "" {
			go func() {
				dcontext.GetLoggerWithField(ctx, "address", debugAddr).Info("debug server listening")
				if err := http.ListenAndServe(debugAddr, nil); err != nil {
					dcontext.GetLoggerWithField(ctx, "error", err).Fatal("error listening on debug interface")
				}
			}()
		}

		err = storage.MarkAndSweep(ctx, driver, registry, storage.GCOpts{
			DryRun:         dryRun,
			RemoveUntagged: removeUntagged,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to garbage collect: %v", err)
			os.Exit(1)
		}
	},
}

// DBCmd is the root of the `database` command.
var DBCmd = &cobra.Command{
	Use:   "database",
	Short: "Manages the registry metadata database",
	Long:  "Manages the registry metadata database",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

// MigrateCmd is the `migrate` sub-command of `database` that manages database migrations.
var MigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Manage migrations",
	Long:  "Manage migrations",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}

var MigrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply up migrations",
	Long:  "Apply up migrations",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		if maxNumMigrations == nil {
			var all int
			maxNumMigrations = &all
		} else if *maxNumMigrations < 1 {
			fmt.Fprintf(os.Stderr, "limit must be greater than or equal to 1")
			os.Exit(1)
		}

		db, err := dbFromConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct database connection: %v", err)
			os.Exit(1)
		}

		m := migrations.NewMigrator(db.DB)
		if skipPostDeployment {
			migrations.SkipPostDeployment(m)
		}

		plan, err := m.UpNPlan(*maxNumMigrations)
		fmt.Println(strings.Join(plan, "\n"))

		if !dryRun {
			n, err := m.UpN(*maxNumMigrations)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to run database migrations: %v", err)
				os.Exit(1)
			}
			fmt.Printf("OK: applied %d migrations\n", n)
		}
	},
}

var MigrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Apply down migrations",
	Long:  "Apply down migrations",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		if maxNumMigrations == nil {
			var all int
			maxNumMigrations = &all
		} else if *maxNumMigrations < 1 {
			fmt.Fprintf(os.Stderr, "limit must be greater than or equal to 1")
			os.Exit(1)
		}

		db, err := dbFromConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct database connection: %v", err)
			os.Exit(1)
		}

		m := migrations.NewMigrator(db.DB)
		plan, err := m.DownNPlan(*maxNumMigrations)
		fmt.Println(strings.Join(plan, "\n"))

		if !dryRun && len(plan) > 0 {
			if !force {
				var response string
				fmt.Print("Preparing to apply the above down migrations. Are you sure? [y/N] ")
				_, err := fmt.Scanln(&response)
				if err != nil && errors.Is(err, io.EOF) {
					fmt.Fprintf(os.Stderr, "failed to scan user input: %v", err)
					os.Exit(1)
				}
				if !regexp.MustCompile(`(?i)^y(es)?$`).MatchString(response) {
					return
				}
			}

			n, err := m.DownN(*maxNumMigrations)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to run database migrations: %v", err)
				os.Exit(1)
			}
			fmt.Printf("OK: applied %d migrations\n", n)
		}
	},
}

// MigrateVersionCmd is the `version` sub-command of `database migrate` that shows the current migration version.
var MigrateVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show current migration version",
	Long:  "Show current migration version",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		db, err := dbFromConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct database connection: %v", err)
			os.Exit(1)
		}

		m := migrations.NewMigrator(db.DB)
		v, err := m.Version()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to detect database version: %v", err)
			os.Exit(1)
		}
		if v == "" {
			v = "Unknown"
		}

		fmt.Printf("%s\n", v)
	},
}

// MigrateStatusCmd is the `status` sub-command of `database migrate` that shows the migrations status.
var MigrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status",
	Long:  "Show migration status",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		db, err := dbFromConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct database connection: %v", err)
			os.Exit(1)
		}

		m := migrations.NewMigrator(db.DB)
		statuses, err := m.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to detect database status: %v", err)
			os.Exit(1)
		}

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Migration", "Applied"})
		table.SetColWidth(80)

		// Display table rows sorted by migration ID
		var ids []string
		for id := range statuses {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		for _, id := range ids {
			name := id
			if statuses[id].Unknown {
				name += " (unknown)"
			}

			if statuses[id].PostDeployment {
				name += " (post deployment)"
			}

			var appliedAt string
			if statuses[id].AppliedAt != nil {
				appliedAt = statuses[id].AppliedAt.String()
			}

			table.Append([]string{name, appliedAt})
		}

		table.Render()
	},
}

// ImportCmd is the `import` sub-command of `database` that imports metadata from the filesystem into the database.
var ImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import filesystem metadata into the database",
	Long: "Import filesystem metadata into the database. This should only be\n" +
		"used for an one-off migration, starting with an empty database.\n" +
		"By default, dangling blobs and untagged manifests are not imported.\n " +
		"Imports may be retried with the --continue-import flag set to true.\n " +
		"This tool can not be used with the parallelwalk storage configuration enabled.",
	Run: func(cmd *cobra.Command, args []string) {
		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		parameters := config.Storage.Parameters()
		if parameters["parallelwalk"] == true {
			parameters["parallelwalk"] = false
			logrus.Info("the 'parallelwalk' configuration parameter has been disabled")
		}

		driver, err := factory.Create(config.Storage.Type(), parameters)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct %s driver: %v", config.Storage.Type(), err)
			os.Exit(1)
		}

		ctx := dcontext.Background()
		ctx, err = configureLogging(ctx, config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to configure logging with config: %s", err)
			os.Exit(1)
		}

		k, err := libtrust.GenerateECP256PrivateKey()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		registry, err := storage.NewRegistry(ctx, driver, storage.Schema1SigningKey(k))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct registry: %v", err)
			os.Exit(1)
		}

		db, err := dbFromConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to construct database connection: %v", err)
			os.Exit(1)
		}

		var opts []datastore.ImporterOption
		if importDanglingBlobs {
			opts = append(opts, datastore.WithImportDanglingBlobs)
		}
		if importDanglingManifests {
			opts = append(opts, datastore.WithImportDanglingManifests)
		}
		if dryRun {
			opts = append(opts, datastore.WithDryRun)
		}
		if !continueImport {
			opts = append(opts, datastore.WithRequireEmptyDatabase)
		}

		p := datastore.NewImporter(db, driver, registry, opts...)

		if repoPath == "" {
			err = p.ImportAll(ctx)
		} else {
			err = p.Import(ctx, repoPath)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to import metadata: %v", err)
			os.Exit(1)
		}
	},
}
