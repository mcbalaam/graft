package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const githubAPI = "https://api.github.com"

// ghHTTP bounds GitHub API calls so a hung connection can't stall the CLI.
var ghHTTP = &http.Client{Timeout: 30 * time.Second}

// githubAuthUser validates a token via GET /user and returns the login.
func githubAuthUser(token string) (string, error) {
	req, err := http.NewRequest("GET", githubAPI+"/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := ghHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub rejected the token: %s", resp.Status)
	}
	var body struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("cannot parse GitHub response: %w", err)
	}
	return body.Login, nil
}

// createRemoteRepo creates a repo via the GitHub API.
// Returns whether the repo was actually created (false when it already existed).
func createRemoteRepo(token, name string, public bool) (bool, error) {
	body := fmt.Sprintf(`{"name":%q,"private":%t}`, name, !public)
	req, err := http.NewRequest("POST", githubAPI+"/user/repos", bytes.NewBufferString(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ghHTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		return true, nil
	case http.StatusUnprocessableEntity:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected response: %s", resp.Status)
	}
}

// deleteRemoteRepo deletes a repo previously created via the API
// (compensation when a later step fails).
func deleteRemoteRepo(token, owner, name string) error {
	req, err := http.NewRequest("DELETE", githubAPI+"/repos/"+owner+"/"+name, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := ghHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected response: %s", resp.Status)
	}
	return nil
}

// ownerFromBaseURL extracts the GitHub owner from a BaseURL:
// git@github.com:user → user, https://github.com/user → user.
func ownerFromBaseURL(base string) string {
	s := strings.TrimSuffix(base, "/")
	if i := strings.LastIndex(s, "/"); i != -1 {
		return s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i != -1 {
		return s[i+1:]
	}
	return s
}
