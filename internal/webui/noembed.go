//go:build !embedui

// Package webui provides the embedded frontend.
// This file is used when the embedui build tag is not set.
package webui

import "embed"

// Dist is an empty filesystem when embedui tag is not set.
var Dist embed.FS
