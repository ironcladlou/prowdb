package hive

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func newDBCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "db",
		Short: "Hive database functions.",
	}

	command.AddCommand(newCreateCommand())

	return command
}

type createOptions struct {
	KubeContext string
	OutputFile  string
	DryRun      bool
}

func newCreateCommand() *cobra.Command {
	var options createOptions

	var command = &cobra.Command{
		Use:   "create",
		Short: "Creates or updates a sqlite database of with Hive cluster claim data.",
		Run: func(cmd *cobra.Command, args []string) {
			err := create(options, context.TODO())
			if err != nil {
				panic(err)
			}
		},
	}

	command.Flags().StringVarP(&options.KubeContext, "context", "c", "", "kubeconfig context to connect to")
	command.Flags().StringVarP(&options.OutputFile, "output-file", "f", path.Join(os.Getenv("HOME"), ".dowser.db"), "output database file location")
	command.Flags().BoolVarP(&options.DryRun, "dry-run", "", false, "output data and exit without writing")

	return command
}

func create(opts createOptions, ctx context.Context) error {
	cfg, err := config.GetConfigWithContext(opts.KubeContext)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	cfg.QPS = 100
	cfg.Burst = 100

	scheme := runtime.NewScheme()
	hivev1.AddToScheme(scheme)

	client, err := crclient.New(cfg, crclient.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to make kube client: %w", err)
	}
	claims := hivev1.ClusterClaimList{}
	err = client.List(ctx, &claims, crclient.InNamespace("hypershift-cluster-pool"))
	if err != nil {
		return fmt.Errorf("failed to list clusterclaims: %w", err)
	}

	db, err := sql.Open("sqlite3", opts.OutputFile)
	if err != nil {
		return err
	}
	defer db.Close()

	sqlStmt := `
create table if not exists claims (
  name text not null primary key,
	claim text
);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
insert or replace into claims(name, claim)
values(?, ?)
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, claim := range claims.Items {
		claimJSON, err := json.Marshal(claim)
		if err != nil {
			return fmt.Errorf("failed to serialize claim: %w", err)
		}
		_, err = stmt.Exec(claim.Name, string(claimJSON))
		if err != nil {
			log.Printf("error inserting claim: %v", err)
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	log.Printf("wrote %d claims", len(claims.Items))
	return nil
}
