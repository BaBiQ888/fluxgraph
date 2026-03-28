package security

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/FluxGraph/fluxgraph/core"
)

var (
	// Basic regex for common prompt injection patterns
	injectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)ignore (all )?previous instructions`),
		regexp.MustCompile(`(?i)system prompt`),
		regexp.MustCompile(`(?i)instead of what I just said`),
		regexp.MustCompile(`(?i)you are now a`),
		regexp.MustCompile(`(?i)new role`),
	}
)

// SanitizeMessage checks for common prompt injection risks across all text parts.
func SanitizeMessage(m core.Message) error {
	for _, part := range m.Parts {
		if part.Type == core.PartTypeText {
			if err := SanitizeInput(part.Text); err != nil {
				return err
			}
		}
	}
	return nil
}

// SanitizeInput checks for common prompt injection risks in a string.
func SanitizeInput(text string) error {
	for _, p := range injectionPatterns {
		if p.MatchString(text) {
			return fmt.Errorf("potential prompt injection detected: matching pattern %s", p.String())
		}
	}
	
	// Check for extremely long inputs that might be used for buffer overflow / DOS on LLM
	if len(text) > 50000 {
		return fmt.Errorf("input text exceeds maximum security limit (50k chars)")
	}
	
	return nil
}

// StripSensitiveChars removes characters often used in terminal escapes or complex injections.
func StripSensitiveChars(text string) string {
	// Simple example: strip control characters except tab/newline
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, text)
}
