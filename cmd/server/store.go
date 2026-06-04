package main

import (
	"log"
	"os"

	"github.com/cuxtud/morpheus-migration-utility/internal/db"
	"github.com/cuxtud/morpheus-migration-utility/internal/profiles"
)

var profileRepo profiles.Repository

func initProfileStore() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		sqlDB, err := db.Open(databaseURL)
		if err != nil {
			return err
		}
		pg := profiles.NewPostgresRepository(sqlDB)
		if err := pg.ImportFromFileIfEmpty(profiles.DefaultFile); err != nil {
			log.Printf("warning: profile import from file: %v", err)
		}
		profileRepo = pg
		log.Printf("Using PostgreSQL JSONB for all storage (profiles, discoveries, migrations, sessions)")
		return nil
	}

	fileRepo, err := profiles.NewFileRepository(profiles.DefaultFile)
	if err != nil {
		return err
	}
	profileRepo = fileRepo
	log.Printf("DATABASE_URL not set — using %s (in-memory discovery cache)", profiles.DefaultFile)
	return nil
}
