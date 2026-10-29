//go:build embedui

// Package webui embeds the frontend build output.
package webui

import "embed"

//go:embed dist/*
var Dist embed.FS
