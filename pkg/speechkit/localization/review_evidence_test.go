package localization

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

// reviewEvidencePath is relative to this package directory, which is the
// working directory of `go test`. The file is part of the public export
// (scripts/public/export-manifest.txt) so this test passes in the mirror too.
const reviewEvidencePath = "../../../docs/localization/review-evidence.md"

type evidenceRow struct {
	Locale      string
	Catalog     string
	SHA256      string
	ReviewState string
	Reviewer    string
	ReviewedOn  string
	Notes       string
}

// parseReviewEvidence reads the first pipe table with the seven evidence
// columns. Header and separator rows are skipped; any other row must have
// exactly seven cells.
func parseReviewEvidence(r io.Reader) ([]evidenceRow, error) {
	var rows []evidenceRow
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimRight(scanner.Text(), "\r"))
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 || cells[0] == "locale" || strings.Trim(cells[0], "-") == "" {
			continue
		}
		if len(cells) != 7 {
			return nil, fmt.Errorf("evidence row for %q has %d cells, want 7", cells[0], len(cells))
		}
		rows = append(rows, evidenceRow{
			Locale:      cells[0],
			Catalog:     cells[1],
			SHA256:      strings.ToLower(cells[2]),
			ReviewState: cells[3],
			Reviewer:    cells[4],
			ReviewedOn:  cells[5],
			Notes:       cells[6],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// catalogDigest hashes a catalog with line endings normalized to LF so a
// CRLF checkout and the LF export agree.
func catalogDigest(raw []byte) string {
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

func loadReviewEvidence(t *testing.T) []evidenceRow {
	t.Helper()
	f, err := os.Open(reviewEvidencePath)
	if err != nil {
		t.Fatalf("open review evidence: %v (the file ships with the source and the public export)", err)
	}
	defer f.Close()
	rows, err := parseReviewEvidence(f)
	if err != nil {
		t.Fatalf("parse review evidence: %v", err)
	}
	return rows
}

func TestReviewEvidenceCoversEveryCatalog(t *testing.T) {
	rows := loadReviewEvidence(t)
	seen := map[string]int{}
	for _, row := range rows {
		seen[row.Locale]++
	}
	for _, locale := range supportedLocales {
		if seen[locale] != 1 {
			t.Errorf("locale %q has %d evidence rows, want exactly 1", locale, seen[locale])
		}
	}
	for locale := range seen {
		known := false
		for _, supported := range supportedLocales {
			if supported == locale {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("evidence row for %q names a locale that is not shipped", locale)
		}
	}
}

func TestReviewEvidenceHashesMatchEmbeddedCatalogs(t *testing.T) {
	for _, row := range loadReviewEvidence(t) {
		raw, err := fs.ReadFile(catalogFiles, "catalogs/"+row.Locale+".json")
		if err != nil {
			t.Errorf("locale %q: read embedded catalog: %v", row.Locale, err)
			continue
		}
		if row.Catalog != "catalogs/"+row.Locale+".json" {
			t.Errorf("locale %q: catalog column %q, want catalogs/%s.json", row.Locale, row.Catalog, row.Locale)
		}
		if got := catalogDigest(raw); got != row.SHA256 {
			t.Errorf("locale %q: catalog sha256 %s does not match the evidence row %s — the catalog changed without a review record; run `go run ./tools/localizationevidence -write`, then have the locale reviewed and upgrade its row", row.Locale, got, row.SHA256)
		}
	}
}

func TestReviewEvidenceStatesAreTruthful(t *testing.T) {
	for _, row := range loadReviewEvidence(t) {
		switch row.ReviewState {
		case "human-reviewed":
			if row.Reviewer == "" || row.Reviewer == "-" {
				t.Errorf("locale %q is human-reviewed but names no reviewer", row.Locale)
			}
			if _, err := time.Parse("2006-01-02", row.ReviewedOn); err != nil {
				t.Errorf("locale %q is human-reviewed but reviewed_on %q is not a calendar day", row.Locale, row.ReviewedOn)
			}
		case "proposal":
			if strings.TrimSpace(row.Notes) == "" || row.Notes == "-" {
				t.Errorf("locale %q is a proposal but the notes do not say what is outstanding", row.Locale)
			}
		default:
			t.Errorf("locale %q has review_state %q, want human-reviewed or proposal", row.Locale, row.ReviewState)
		}
	}
}

func TestCatalogDigestNormalizesLineEndings(t *testing.T) {
	if catalogDigest([]byte("{\r\n \"a\": 1\r\n}\r\n")) != catalogDigest([]byte("{\n \"a\": 1\n}\n")) {
		t.Fatal("CRLF and LF catalogs must hash identically")
	}
}
