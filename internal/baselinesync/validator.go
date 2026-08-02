package baselinesync

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

func validateDatabase(path, version string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve candidate database: %w", err)
	}
	dsn := sqliteURI(abs, url.Values{"mode": {"ro"}, "immutable": {"1"}})
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open candidate database: %w", err)
	}
	defer db.Close()
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || !strings.EqualFold(check, "ok") {
		return fmt.Errorf("candidate database quick_check failed: %v", err)
	}
	var table string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'spam_numbers'`).Scan(&table); err != nil {
		return errors.New("candidate database is missing spam_numbers")
	}
	if err := requireColumns(db, "spam_numbers", []string{"phone_number", "tag", "source"}); err != nil {
		return err
	}
	if err := requireIndex(db, "spam_numbers"); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'metadata'`).Scan(&table); err != nil {
		return errors.New("candidate database is missing metadata")
	}
	if err := requireColumns(db, "metadata", []string{"key", "value"}); err != nil {
		return err
	}
	var actual string
	if err := db.QueryRow(`SELECT value FROM metadata WHERE key = 'version'`).Scan(&actual); err != nil {
		return errors.New("candidate database version metadata is missing")
	}
	if strings.TrimSpace(version) != "" && actual != version {
		return fmt.Errorf("candidate database version %q does not match manifest %q", actual, version)
	}
	return nil
}

func requireIndex(db *sql.DB, table string) error {
	rows, err := db.Query("PRAGMA index_list(" + table + ")")
	if err != nil {
		return fmt.Errorf("read %s indexes: %w", table, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return fmt.Errorf("candidate database is missing an index for %s", table)
	}
	return nil
}

func requireColumns(db *sql.DB, table string, required []string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("read %s columns: %w", table, err)
	}
	defer rows.Close()
	found := make(map[string]struct{}, len(required))
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		found[name] = struct{}{}
	}
	for _, column := range required {
		if _, ok := found[column]; !ok {
			return fmt.Errorf("candidate database is missing %s.%s", table, column)
		}
	}
	return rows.Err()
}

func sqliteURI(path string, query url.Values) string {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := &url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}
	return uri.String()
}

func ensureFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("baseline path is not a regular file")
	}
	return nil
}
