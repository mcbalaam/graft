package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func RunSudo(dir string, args ...string) (string, error) {
	fullArgs := append([]string{"git"}, args...)
	cmd := exec.Command("sudo", fullArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func IsRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

func HasRemote(path string) bool {
	out, err := Run(path, "remote")
	return err == nil && strings.TrimSpace(out) != ""
}

func HasCommits(path string) bool {
	_, err := Run(path, "rev-parse", "HEAD")
	return err == nil
}

func SubmoduleURL(repoPath, name string) (string, error) {
	gitmodulesPath := filepath.Join(repoPath, ".gitmodules")
	if _, err := os.Stat(gitmodulesPath); err != nil {
		return "", fmt.Errorf("no .gitmodules found in %s", repoPath)
	}

	out, err := Run(repoPath, "config", "--file", ".gitmodules",
		fmt.Sprintf("submodule.%s.url", name))
	if err != nil {
		return "", fmt.Errorf("blob '%s' not found in .gitmodules", name)
	}

	return out, nil
}

func ListSubmodules(repoPath string) (map[string]string, error) {
	gitmodulesPath := filepath.Join(repoPath, ".gitmodules")
	if _, err := os.Stat(gitmodulesPath); err != nil {
		return map[string]string{}, nil
	}

	out, err := Run(repoPath, "config", "--file", ".gitmodules", "--get-regexp", "submodule\\..*\\.path")
	if err != nil {
		return map[string]string{}, nil
	}

	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// format as following: submodule.<name>.path <path>
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]  // key
		path := parts[1] // value

		keyParts := strings.Split(key, ".")
		if len(keyParts) < 3 {
			continue
		}
		name := strings.Join(keyParts[1:len(keyParts)-1], ".")
		result[name] = path
	}

	return result, nil
}

func IsSubmodule(repoPath, path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	submodules, err := ListSubmodules(repoPath)
	if err != nil {
		return false
	}

	for _, subPath := range submodules {
		absSubPath, err := filepath.Abs(filepath.Join(repoPath, subPath))
		if err != nil {
			continue
		}
		if absPath == absSubPath {
			return true
		}
	}

	return false
}

func AbsPath(path string) (string, error) {
	return filepath.Abs(path)
}
