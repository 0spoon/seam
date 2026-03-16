package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/userdb"
)

var slugNonAlphanumRe = regexp.MustCompile(`[^a-z0-9-]`)
var slugMultiHyphenRe = regexp.MustCompile(`-{2,}`)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = slugNonAlphanumRe.ReplaceAllString(s, "")
	s = slugMultiHyphenRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func newID() string {
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate ULID: %v", err)
	}
	return id.String()
}

func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

type projectDef struct {
	Name        string
	Description string
}

type noteDef struct {
	Title   string
	Body    string
	Tags    []string
	Project int
}

func main() {
	dataDir := "./data"
	ctx := context.Background()

	serverDB, err := auth.OpenServerDB(filepath.Join(dataDir, "server.db"))
	if err != nil {
		log.Fatalf("open server.db: %v", err)
	}
	defer serverDB.Close()

	store := auth.NewSQLStore(serverDB)
	if _, err := store.GetUserByUsername(ctx, "demo"); err == nil {
		log.Fatal("demo user already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("demo"), 4)
	if err != nil {
		log.Fatalf("bcrypt: %v", err)
	}

	userID := newID()
	now := time.Now().UTC()
	if err := store.CreateUser(ctx, &auth.User{
		ID:        userID,
		Username:  "demo",
		Email:     "demo@seam.local",
		Password:  string(hash),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		log.Fatalf("create user: %v", err)
	}
	fmt.Printf("created user: demo (id=%s)\n", userID)

	mgr := userdb.NewSQLManager(dataDir, 30*time.Minute, nil)
	defer mgr.CloseAll()

	userDB, err := mgr.Open(ctx, userID)
	if err != nil {
		log.Fatalf("open user db: %v", err)
	}
	notesDir := mgr.UserNotesDir(userID)

	projects := []projectDef{
		{Name: "Cortical Decoder", Description: "Motor cortex neural decoder for real-time prosthetic control. ECoG signal processing pipeline and online calibration."},
		{Name: "NeuroLink SDK", Description: "Commercial SDK for third-party BCI application developers. REST API, streaming protocol, and device abstraction layer."},
		{Name: "Sensory Feedback Loop", Description: "Closed-loop somatosensory stimulation research. Micro-stimulation patterns mapped to tactile percepts for prosthetic hands."},
		{Name: "Clinical Trials Program", Description: "Managing multi-site clinical trials for the implantable BCI. Participant recruitment, protocol design, data collection, adverse event tracking."},
		{Name: "Regulatory Affairs", Description: "FDA submissions, EU MDR compliance, ISO 13485 quality management, risk analysis documentation."},
		{Name: "Electrode Array Manufacturing", Description: "Cleanroom fabrication of micro-electrode arrays. Quality control, yield optimization, biocompatibility testing."},
		{Name: "Neural Signal AI", Description: "Deep learning models for neural signal decoding. Transformer architectures, training pipelines, spike sorting, transfer learning across participants."},
		{Name: "BCI Cloud Platform", Description: "Cloud infrastructure for multi-site BCI research. Real-time telemetry, data warehousing, remote session monitoring, analytics dashboards."},
		{Name: "Data Privacy and Security", Description: "Protecting neural data. HIPAA compliance, data anonymization, encryption protocols, IRB data handling requirements, neurorights considerations."},
		{Name: "Non-Invasive EEG Headset", Description: "Consumer-grade dry-electrode EEG headset for BCI applications. Hardware design, signal processing for noisy environments, SSVEP and P300 paradigms."},
		{Name: "Spinal Cord Interface", Description: "Epidural spinal cord stimulation for restoring locomotion. Electrode placement, stimulation patterns, gait cycle decoding from residual motor signals."},
		{Name: "Implant Telemetry System", Description: "Wireless data and power transfer for implanted BCI devices. RF link design, firmware, over-the-air updates, thermal management."},
		{Name: "Speech Prosthesis", Description: "Decoding attempted speech from motor cortex neural activity. Phoneme classification, language model integration, real-time speech synthesis for locked-in patients."},
		{Name: "BCI Gaming Platform", Description: "Commercial gaming platform using BCI input. Low-latency intent classification, game controller abstraction, multiplayer support, accessibility focus."},
		{Name: "Neurorehab Therapy Suite", Description: "Software platform for BCI-driven neurorehabilitation. Motor recovery tracking, adaptive difficulty, gamified exercises, clinician dashboards."},
	}

	projectIDs := make([]string, len(projects))
	projectSlugs := make([]string, len(projects))

	for i, p := range projects {
		id := newID()
		slug := slugify(p.Name)
		projectIDs[i] = id
		projectSlugs[i] = slug

		if err := os.MkdirAll(filepath.Join(notesDir, slug), 0o755); err != nil {
			log.Fatalf("mkdir project %s: %v", slug, err)
		}

		ts := now.Add(-180 * 24 * time.Hour).Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		if _, err := userDB.ExecContext(ctx,
			`INSERT INTO projects (id, name, slug, description, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
			id, p.Name, slug, p.Description, ts, ts,
		); err != nil {
			log.Fatalf("insert project %s: %v", p.Name, err)
		}
		fmt.Printf("  project[%d]: %s\n", i, p.Name)
	}

	allNotes := append(coreNotes(), extraNotes...)

	tx, err := userDB.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}

	usedPaths := make(map[string]bool)

	for i, n := range allNotes {
		if n.Project < 0 || n.Project >= len(projects) {
			log.Fatalf("note %q: invalid project index %d", n.Title, n.Project)
		}

		noteID := newID()
		ts := now.Add(-time.Duration(len(allNotes)-i) * time.Hour)

		projSlug := projectSlugs[n.Project]
		projID := sql.NullString{String: projectIDs[n.Project], Valid: true}

		fm := &note.Frontmatter{
			ID:       noteID,
			Title:    n.Title,
			Project:  projSlug,
			Tags:     n.Tags,
			Created:  ts,
			Modified: ts,
		}
		content, err := note.SerializeFrontmatter(fm, n.Body)
		if err != nil {
			log.Fatalf("serialize %q: %v", n.Title, err)
		}

		noteSlug := slugify(n.Title)
		relPath := projSlug + "/" + noteSlug + ".md"
		if usedPaths[relPath] {
			relPath = projSlug + "/" + noteSlug + fmt.Sprintf("-%d", i) + ".md"
		}
		usedPaths[relPath] = true

		absPath := filepath.Join(notesDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			log.Fatalf("mkdir for %q: %v", n.Title, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			log.Fatalf("write %q: %v", n.Title, err)
		}

		tsStr := ts.Format(time.RFC3339)
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO notes (id, title, project_id, file_path, body, content_hash,
			 source_url, transcript_source, slug, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, NULL, 0, ?, ?, ?)`,
			noteID, n.Title, projID, relPath, n.Body, computeHash(content),
			noteSlug, tsStr, tsStr,
		); err != nil {
			log.Fatalf("insert note %q: %v", n.Title, err)
		}

		for _, tag := range note.ParseTags(n.Body, n.Tags) {
			_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag)
			var tagID int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, tag).Scan(&tagID); err != nil {
				log.Fatalf("select tag %q: %v", tag, err)
			}
			_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_tags (note_id, tag_id) VALUES (?, ?)`, noteID, tagID)
		}

		for _, link := range note.ParseWikilinks(n.Body) {
			_, _ = tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO links (source_note_id, target_note_id, link_text) VALUES (?, NULL, ?)`,
				noteID, link.Target,
			)
		}

		if (i+1)%50 == 0 {
			fmt.Printf("  ... %d/%d notes inserted\n", i+1, len(allNotes))
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT source_note_id, link_text FROM links WHERE target_note_id IS NULL`)
	if err != nil {
		log.Fatalf("query dangling links: %v", err)
	}
	type dl struct{ src, text string }
	var dangling []dl
	for rows.Next() {
		var d dl
		if err := rows.Scan(&d.src, &d.text); err != nil {
			log.Fatalf("scan link: %v", err)
		}
		dangling = append(dangling, d)
	}
	rows.Close()

	resolved := 0
	for _, d := range dangling {
		var targetID string
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM notes WHERE LOWER(title) = LOWER(?) LIMIT 1`, d.text,
		).Scan(&targetID); err == nil {
			if _, err = tx.ExecContext(ctx,
				`UPDATE links SET target_note_id = ? WHERE source_note_id = ? AND link_text = ?`,
				targetID, d.src, d.text,
			); err != nil {
				log.Fatalf("resolve link: %v", err)
			}
			resolved++
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("commit: %v", err)
	}

	fmt.Printf("\ndone: 1 user, %d projects, %d notes, %d/%d wikilinks resolved\n",
		len(projects), len(allNotes), resolved, len(dangling))
	fmt.Println("login with demo:demo")
}
