package prompt

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Secret prints a prompt and reads a hidden (no echo) line from the terminal.
// Returns an empty string when not interactive.
func Secret(question string) (string, error) {
	fmt.Print(question)
	if !isInteractive() {
		fmt.Println()
		return "", nil
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("✗ cannot read input: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
