// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// relui is a web interface for managing the release process of Go.
package main

import (
	"context"
	"flag"
	"log"
	"net/url"

	"github.com/jackc/pgx/v4/pgxpool"
	"golang.org/x/build/internal/https"
	"golang.org/x/build/internal/relui"
	"golang.org/x/build/internal/secret"
	"golang.org/x/build/internal/task"
)

var (
	baseURL       = flag.String("base-url", "", "Prefix URL for routing and links.")
	siteTitle     = flag.String("site-title", "Go Releases", "Site title.")
	siteHeaderCSS = flag.String("site-header-css", "", "Site header CSS class name. Can be used to pick a look for the header.")

	downUp      = flag.Bool("migrate-down-up", false, "Run all Up migration steps, then the last down migration step, followed by the final up migration. Exits after completion.")
	migrateOnly = flag.Bool("migrate-only", false, "Exit after running migrations. Migrations are run by default.")
	pgConnect   = flag.String("pg-connect", "", "Postgres connection string or URI. If empty, libpq connection defaults are used.")
)

func main() {
	if err := secret.InitFlagSupport(context.Background()); err != nil {
		log.Fatalln(err)
	}
	var twitterAPI secret.TwitterCredentials
	secret.JSONVarFlag(&twitterAPI, "twitter-api-secret", "Twitter API secret to use for workflows involving tweeting.")
	https.RegisterFlags(flag.CommandLine)
	flag.Parse()

	ctx := context.Background()
	if err := relui.InitDB(ctx, *pgConnect); err != nil {
		log.Fatalf("relui.InitDB() = %v", err)
	}
	if *migrateOnly {
		return
	}
	if *downUp {
		if err := relui.MigrateDB(*pgConnect, true); err != nil {
			log.Fatalf("relui.MigrateDB() = %v", err)
		}
		return
	}

	// Define the site header and external service configuration.
	// The site header communicates to humans what will happen
	// when workflows run.
	// Keep these appropriately in sync.
	var (
		siteHeader = relui.SiteHeader{
			Title:    *siteTitle,
			CSSClass: *siteHeaderCSS,
		}
		extCfg = task.ExternalConfig{
			// TODO(go.dev/issue/51150): When twitter client creation is factored out from task package, update code here.
			TwitterAPI: twitterAPI,
		}
	)

	dh := relui.NewDefinitionHolder()
	relui.RegisterTweetDefinitions(dh, extCfg)
	db, err := pgxpool.Connect(ctx, *pgConnect)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	w := relui.NewWorker(dh, db, relui.NewPGListener(db))
	go w.Run(ctx)
	if err := w.ResumeAll(ctx); err != nil {
		log.Printf("w.ResumeAll() = %v", err)
	}
	var base *url.URL
	if *baseURL != "" {
		base, err = url.Parse(*baseURL)
		if err != nil {
			log.Fatalf("url.Parse(%q) = %v, %v", *baseURL, base, err)
		}
	}
	s := relui.NewServer(db, w, base, siteHeader)
	if err != nil {
		log.Fatalf("relui.NewServer() = %v", err)
	}
	log.Fatalln(https.ListenAndServe(ctx, s))
}
