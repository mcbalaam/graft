package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Ask prints a prompt and returns the user's input string (empty string if not interactive).
func Ask(question string) (string, error) {
	fmt.Print(question)
	if !isInteractive() {
		fmt.Println()
		return "", nil
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	return "", nil
}

func Query(question string, options []string, defaultChoice int) (int, error) {
	fmt.Println(question)
	if !isInteractive() {
		if defaultChoice == -1 {
			return -1, fmt.Errorf("✗ not an interactive session and no defaults for this option")
		}
		fmt.Println("● not an interactive session: defaulting to", options[defaultChoice])
		return defaultChoice, nil
	}

	for i, opt := range options {
		fmt.Printf("  [%d] %s\n", i+1, opt)
	}
	fmt.Print("> ")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		for i := range options {
			if input == fmt.Sprintf("%d", i+1) {
				return i, nil
			}
		}
		fmt.Print("✗ invalid choice, try again\n> ")
	}
	return -1, fmt.Errorf("no input")
}
