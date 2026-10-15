package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/albievan/cactusinv/internal/adapters/agecrypt"
	"github.com/albievan/cactusinv/internal/adapters/backupdir"
	"github.com/albievan/cactusinv/internal/adapters/backupformat"
	"github.com/albievan/cactusinv/internal/adapters/mariadb"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// passphraseEnv may carry a backup passphrase for the CLI (instead of --passphrase-file);
// it is never logged.
const passphraseEnv = "CACTUSINV_BACKUP_PASSPHRASE"

// backupDeps wires backups and restores (ADR-010). Missing pieces switch features off
// with a warning; a malformed backup_key or a misconfigured staging database is an error.
// The returned func closes the staging pool.
func backupDeps(ctx context.Context, cfg config, db *mariadbDB, dbCfg mariadb.Config, log *slog.Logger) (app.BackupDeps, func(), error) {
	d := app.BackupDeps{
		Snapshotter: mariadb.NewSnapshotter(db), Codec: backupformat.New(backupformat.Limits{}),
		Times: cfg.BackupTimes, Location: cfg.BackupLocation, RetentionDays: cfg.BackupRetentionDays,
		AppVersion: version, Build: build,
	}
	key, err := cfg.optionalSecret(ctx, "backup_key")
	if err != nil {
		return d, func() {}, err
	}
	sealer, err := agecrypt.New(key)
	if err != nil {
		return d, func() {}, err
	}
	d.Sealer = sealer
	if !sealer.KeyConfigured() {
		log.Warn("no backup_key secret: stored and scheduled backups are off (downloads with a passphrase still work)")
	}
	if err := os.MkdirAll(cfg.BackupDir, 0o700); err != nil {
		log.Warn("backups directory unavailable: stored backups and restores are off", "dir", cfg.BackupDir, "err", err)
	} else if files, err := backupdir.New(cfg.BackupDir); err != nil {
		log.Warn("backups directory unavailable: stored backups and restores are off", "dir", cfg.BackupDir, "err", err)
	} else {
		d.Files = files
	}
	stagingCfg := dbCfg
	stagingCfg.Name = cfg.RestoreDBName
	staging, err := mariadb.OpenLazy(stagingCfg)
	if err != nil {
		return d, func() {}, err
	}
	stager, err := mariadb.NewStager(ctx, db, staging, cfg.DBName, cfg.RestoreDBName)
	if err != nil {
		_ = staging.Close()
		return d, func() {}, err
	}
	if err := stager.Ready(ctx); err != nil {
		log.Warn("restore staging database not available yet: restores are off until it is created and granted (deploy.sh --install)", "database", cfg.RestoreDBName)
	}
	d.Stager = stager
	return d, func() { _ = staging.Close() }, nil
}

// parseInterleaved parses flags that may come before or after positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// openPath opens a file named on the command line (through an os.Root on its directory).
func openPath(p string, create bool) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(p))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if create {
		return root.OpenFile(filepath.Base(p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	return root.Open(filepath.Base(p))
}

func readPassphrase(file string) (string, error) {
	if file != "" {
		f, err := openPath(file, false)
		if err != nil {
			return "", fmt.Errorf("passphrase file: %w", err)
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, domain.MaxBackupPassphraseLength+2))
		if err != nil {
			return "", fmt.Errorf("passphrase file: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return os.Getenv(passphraseEnv), nil
}

const backupUsage = `usage:
  cactusinv backup keygen                              print a new backup_key (keep a copy off the server)
  cactusinv backup create [--out FILE --passphrase-file P]
                                                       store a backup (server key), or write one encrypted with a passphrase to FILE
  cactusinv backup list                                list stored backups
  cactusinv backup restore NAME|FILE|- [--passphrase-file P] [--yes]
                                                       stage a stored backup (NAME) or a file (FILE, - for stdin), show what
                                                       restoring does, then apply it (--yes, or type RESTORE when asked)
  cactusinv backup stage NAME|FILE|- [--passphrase-file P]
                                                       only stage and show the preview; prints restore-id: ID
  cactusinv backup apply ID --yes | discard ID         apply or discard a staged restore
  cactusinv backup note-legacy-restore FILE|- [--name NAME]
                                                       record in the audit log that a .sql.gz dump was restored
The passphrase may also come from $` + passphraseEnv + `.`

// backupCommand runs `cactusinv backup …`. interactive says stdin is a terminal (the
// restore may then ask for confirmation).
func backupCommand(ctx context.Context, cfg config, args []string, stdin io.Reader, stdout io.Writer, interactive bool) error {
	if len(args) == 0 {
		return errors.New(backupUsage)
	}
	if args[0] == "cat-legacy" { // used by deploy.sh: no database needed
		if len(args) != 2 {
			return errors.New("usage: cactusinv backup cat-legacy NAME")
		}
		return catLegacy(ctx, cfg.BackupDir, args[1], stdout)
	}
	if args[0] == "keygen" {
		key, err := agecrypt.GenerateKey()
		if err != nil {
			return err
		}
		s, err := agecrypt.New(key)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "# cactusInv backup_key, public key %s\n%s", s.Recipient(), key)
		return err
	}
	fs := flag.NewFlagSet("backup "+args[0], flag.ContinueOnError)
	out := fs.String("out", "", "write a passphrase-encrypted backup to this file instead of storing one")
	passFile := fs.String("passphrase-file", "", "file holding the passphrase")
	yes := fs.Bool("yes", false, "apply the restore without asking")
	legacyName := fs.String("name", "", "the dump's file name when it is read from stdin")
	pos, err := parseInterleaved(fs, args[1:])
	if err != nil {
		return err
	}
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	svc, closeSvc, err := newService(ctx, cfg, db, newLogger("error"))
	if err != nil {
		return err
	}
	defer closeSvc()
	ctx = app.WithActor(ctx, app.ActorOperator)

	switch {
	case args[0] == "create" && len(pos) == 0:
		if *out == "" {
			b, err := svc.CreateBackup(ctx, domain.BackupManual)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "%s  %d bytes  sha256 %s\n", b.Name, b.Size, b.SHA256)
			return err
		}
		return exportToFile(ctx, svc, *out, *passFile, stdout)
	case args[0] == "list" && len(pos) == 0:
		return listBackups(ctx, svc, stdout)
	case args[0] == "restore" && len(pos) == 1:
		p, err := stageCommand(ctx, svc, pos[0], *passFile, stdin, stdout)
		if err != nil {
			return err
		}
		if !*yes && !(interactive && pos[0] != "-" && confirmRestore(stdin, stdout)) {
			if err := svc.DiscardRestore(ctx, p.ID); err != nil {
				return err
			}
			return errors.New("not restored: confirm by typing RESTORE, or run again with --yes")
		}
		return applyCommand(ctx, svc, p.ID, stdout)
	case args[0] == "stage" && len(pos) == 1:
		p, err := stageCommand(ctx, svc, pos[0], *passFile, stdin, stdout)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "restore-id: %s\n", p.ID)
		return err
	case args[0] == "apply" && len(pos) == 1:
		if !*yes {
			return errors.New("apply replaces the live data: add --yes")
		}
		return applyCommand(ctx, svc, pos[0], stdout)
	case args[0] == "discard" && len(pos) == 1:
		if err := svc.DiscardRestore(ctx, pos[0]); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, "discarded")
		return err
	case args[0] == "note-legacy-restore" && len(pos) == 1:
		src, name := stdin, *legacyName
		if pos[0] != "-" {
			f, err := openPath(pos[0], false)
			if err != nil {
				return err
			}
			defer f.Close()
			src, name = f, filepath.Base(pos[0])
		}
		if name == "" {
			return errors.New("note-legacy-restore -: give the dump's file name with --name")
		}
		if err := svc.NoteLegacyRestore(ctx, name, src); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "recorded in the audit log")
		return err
	}
	return errors.New(backupUsage)
}

func exportToFile(ctx context.Context, svc *app.Service, out, passFile string, stdout io.Writer) error {
	pass, err := readPassphrase(passFile)
	if err != nil {
		return err
	}
	if pass == "" {
		return fmt.Errorf("--out needs a passphrase (--passphrase-file or $%s)", passphraseEnv)
	}
	f, err := openPath(out, true)
	if err != nil {
		return err
	}
	err = svc.ExportBackup(ctx, pass, func(string) (io.Writer, error) { return f, nil })
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(out)
		return err
	}
	_, err = fmt.Fprintf(stdout, "wrote %s (encrypted with the passphrase)\n", out)
	return err
}

func listBackups(ctx context.Context, svc *app.Service, stdout io.Writer) error {
	ov, err := svc.ListBackups(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "NAME\tKIND\tSCHEMA\tSIZE\tCREATED\n")
	for _, f := range ov.Files {
		kind := string(f.Kind)
		if f.Legacy {
			kind = "legacy (plaintext)"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n", f.Name, kind, f.Schema, f.Size, f.CreatedAt.Format("2006-01-02 15:04 MST"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !ov.KeyConfigured {
		_, _ = fmt.Fprintln(stdout, "no backup_key: stored and scheduled backups are off")
	}
	if ov.RestoreUnavailable != "" {
		_, _ = fmt.Fprintln(stdout, "restores unavailable: "+ov.RestoreUnavailable)
	}
	return nil
}

// catLegacy streams a legacy plaintext dump, by its validated name, from the backups
// directory (deploy.sh pipes it into the database client).
func catLegacy(ctx context.Context, dir, name string, stdout io.Writer) error {
	if !domain.IsLegacyBackupName(name) {
		return errors.New("not a legacy backup name (cactusinv-YYYYMMDD-HHMMSS.sql.gz)")
	}
	files, err := backupdir.New(dir)
	if err != nil {
		return err
	}
	f, err := files.Open(ctx, name)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(stdout, f)
	return err
}

// stageCommand stages NAME, or a file (stored first as an upload, which records its
// SHA-256 in the audit log before anything is staged — ADR-009), and prints the preview.
func stageCommand(ctx context.Context, svc *app.Service, arg, passFile string, stdin io.Reader, stdout io.Writer) (app.RestorePreview, error) {
	pass, err := readPassphrase(passFile)
	if err != nil {
		return app.RestorePreview{}, err
	}
	name := arg
	if _, perr := domain.ParseBackupName(arg); perr != nil {
		src := stdin
		if arg != "-" {
			f, err := openPath(arg, false)
			if err != nil {
				return app.RestorePreview{}, err
			}
			defer f.Close()
			src = f
		}
		up, err := svc.UploadBackup(ctx, src)
		if err != nil {
			return app.RestorePreview{}, err
		}
		name = up.Name
		_, _ = fmt.Fprintf(stdout, "stored the file as %s (%d bytes)\n", name, up.Size)
	}
	p, err := svc.StageRestore(ctx, name, pass)
	if err != nil {
		return app.RestorePreview{}, err
	}
	printPreview(stdout, p)
	return p, nil
}

func printPreview(stdout io.Writer, p app.RestorePreview) {
	_, _ = fmt.Fprintf(stdout, "backup %s, taken %s by %s (version %s), schema %d → %d\n",
		p.Source, p.Header.CreatedAt.Format("2006-01-02 15:04 MST"), p.Header.CreatedBy, p.Header.AppVersion, p.SchemaFrom, p.SchemaTo)
	_, _ = fmt.Fprintf(stdout, "invoices created since the backup (removed by restoring): %d\naudit: %s\n", p.InvoicesSince, p.Audit.Summary())
	for _, t := range p.Tables {
		_, _ = fmt.Fprintf(stdout, "  %-20s %8d rows (now %d)\n", t.Name, t.Backup, t.Live)
	}
	if p.SafetyBackup {
		_, _ = fmt.Fprintln(stdout, "the current data is backed up first (a safety backup), so this can be undone")
	}
	for _, w := range p.Warnings {
		_, _ = fmt.Fprintln(stdout, "warning: "+w)
	}
}

// confirmRestore asks for the word RESTORE on the terminal.
func confirmRestore(stdin io.Reader, stdout io.Writer) bool {
	_, _ = fmt.Fprint(stdout, "Type RESTORE to replace the live data with this backup: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return domain.CheckRestoreConfirmation(line) == nil
}

func applyCommand(ctx context.Context, svc *app.Service, id string, stdout io.Writer) error {
	res, err := svc.ApplyRestoreAsOperator(ctx, id)
	if err != nil {
		return err
	}
	if res.SafetyBackup != "" {
		_, _ = fmt.Fprintf(stdout, "safety backup of the data before: %s (restore it to undo)\n", res.SafetyBackup)
	}
	_, err = fmt.Fprintf(stdout, "restored %s; everyone is signed out and automatic sending is paused until resumed in the app\n", res.Source)
	return err
}
