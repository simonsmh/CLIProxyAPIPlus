// Package common provides shared constants and utilities for Kiro translator.
package common

const (
	// KiroMaxToolDescLen is the maximum description length for Kiro API tools.
	// Kiro API limit is 10240 bytes, leave room for "..."
	KiroMaxToolDescLen = 10237
)
