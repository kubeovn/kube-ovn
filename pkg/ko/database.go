package ko

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func databaseName(role string) string {
	if role == "nb" {
		return "OVN_Northbound"
	}
	return "OVN_Southbound"
}

func databaseCommand(role, action string, args ...string) []string {
	return append([]string{"ovn-appctl", "-t", "/var/run/ovn/ovn" + role + "_db.ctl", action, databaseName(role)}, args...)
}

func (a *Application) addDatabaseCommands() {
	parent := &cobra.Command{Use: "db", Short: "Inspect, back up and recover OVN databases"}
	parent.AddCommand(&cobra.Command{Use: "health", Short: "Check NB and SB storage on every central pod without requiring a leader", Args: cobra.NoArgs, RunE: a.run(a.databaseStatus)})
	for _, role := range []string{"nb", "sb"} {
		command := &cobra.Command{Use: role, Short: "Operate the " + role + " database"}
		command.AddCommand(&cobra.Command{
			Use: "status", Short: "Show cluster and storage status", Args: cobra.NoArgs,
			RunE: a.run(func(ctx context.Context, client *Client, _ []string) error {
				target, err := client.leader(ctx, role)
				if err != nil {
					return err
				}
				for _, action := range []string{"cluster/status", "ovsdb-server/get-db-storage-status"} {
					if err := client.Executor.Exec(ctx, target, databaseCommand(role, action), a.outputStreams()); err != nil {
						return err
					}
				}
				return nil
			}),
		})
		a.addBackupCommand(command, role)
		a.addKickCommand(command, role)
		if role == "nb" {
			a.addRestoreCommand(command)
		}
		parent.AddCommand(command)
	}
	a.root.AddCommand(parent)
}

func (a *Application) databaseStatus(ctx context.Context, client *Client, _ []string) error {
	targets, err := client.targets(ctx, "app=ovn-central", "", "ovn-central", false)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("no running ovn-central containers are available")
	}
	targets, err = client.replaceWithAgents(ctx, targets)
	if err != nil {
		return err
	}
	var failures []error
	for _, target := range targets {
		if _, err := fmt.Fprintf(a.streams.Out, "Database storage on %s\n", target.Pod); err != nil {
			return err
		}
		for _, role := range []string{"nb", "sb"} {
			status, err := client.capture(ctx, target, databaseCommand(role, "ovsdb-server/get-db-storage-status")...)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s %s: %w", target.Pod, role, err))
				continue
			}
			if _, err := fmt.Fprintln(a.streams.Out, status); err != nil {
				return err
			}
			if strings.TrimSpace(status) != "status: ok" {
				failures = append(failures, fmt.Errorf("%s %s storage is unhealthy: %s", target.Pod, role, strings.TrimSpace(status)))
			}
		}
	}
	return errors.Join(failures...)
}

func (a *Application) addKickCommand(parent *cobra.Command, role string) {
	var dryRun bool
	command := &cobra.Command{Use: "kick SERVER_ID", Short: "Remove a stale database cluster member", Args: cobra.ExactArgs(1)}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Resolve and print the target without changing the database")
	command.RunE = a.run(func(ctx context.Context, client *Client, args []string) error {
		resolve := client.leader
		if dryRun {
			resolve = client.leaderPod
		}
		target, err := resolve(ctx, role)
		if err != nil {
			return err
		}
		argv := databaseCommand(role, "cluster/kick", args[0])
		if dryRun {
			_, err := fmt.Fprintf(a.streams.Out, "%s/%s: %q\n", target.Namespace, target.Pod, argv)
			return err
		}
		return client.Executor.Exec(ctx, target, argv, a.outputStreams())
	})
	parent.AddCommand(command)
}

func (a *Application) addBackupCommand(parent *cobra.Command, role string) {
	var output string
	command := &cobra.Command{Use: "backup", Short: "Download a verified standalone database backup", Args: cobra.NoArgs}
	command.Flags().StringVar(&output, "output", "", "Destination file (must not already exist)")
	command.RunE = a.run(func(ctx context.Context, client *Client, _ []string) error {
		target, err := client.leader(ctx, role)
		if err != nil {
			return err
		}
		destination := output
		if destination == "" {
			destination = "ovn" + role + "_db." + runID() + ".backup"
		}
		return a.backup(ctx, client, target, role, destination)
	})
	parent.AddCommand(command)
}

func runID() string { return strings.ToLower(rand.Text()) }

func (a *Application) backup(ctx context.Context, client *Client, target Target, role, destination string) (resultErr error) {
	remote := "/tmp/kubectl-ko-" + role + "-" + runID() + ".backup"
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, err := client.capture(cleanupCtx, target, "rm", "-f", remote)
		resultErr = errors.Join(resultErr, err)
	}()
	if _, err := client.capture(ctx, target, "ovsdb-tool", "cluster-to-standalone", remote, "/etc/ovn/ovn"+role+"_db.db"); err != nil {
		return err
	}
	name, err := client.capture(ctx, target, "ovsdb-tool", "db-name", remote)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) != databaseName(role) {
		return fmt.Errorf("unexpected backup database name %q", name)
	}
	checksum, err := client.downloadFile(ctx, target, remote, destination)
	if err != nil {
		return err
	}
	metadata := struct{ Database, Pod, Namespace, SHA256, File string }{databaseName(role), target.Pod, target.Namespace, checksum, filepath.Base(destination)}
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.streams.Out, "Backed up %s to %s (sha256:%s)\n", databaseName(role), destination, checksum)
	return err
}
