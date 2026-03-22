package credentialsfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SessionCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type Store struct {
	path string
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(homeDir, ".aws", "credentials")
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) UpsertProfile(profile string, creds SessionCredentials) error {
	if strings.TrimSpace(profile) == "" {
		return fmt.Errorf("profile is required")
	}
	if strings.TrimSpace(creds.AccessKeyID) == "" || strings.TrimSpace(creds.SecretAccessKey) == "" || strings.TrimSpace(creds.SessionToken) == "" {
		return fmt.Errorf("session credentials are incomplete")
	}

	lines := []string{}
	if content, err := os.ReadFile(s.path); err == nil {
		lines = splitLines(string(content))
	} else if !os.IsNotExist(err) {
		return err
	}

	newSection := []string{
		fmt.Sprintf("[%s]", profile),
		fmt.Sprintf("aws_access_key_id=%s", creds.AccessKeyID),
		fmt.Sprintf("aws_secret_access_key=%s", creds.SecretAccessKey),
		fmt.Sprintf("aws_session_token=%s", creds.SessionToken),
		"",
	}

	var output []string
	inTarget := false
	wrote := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isSectionHeader(trimmed) {
			current := parseSectionName(trimmed)
			if inTarget {
				output = append(output, newSection...)
				wrote = true
				inTarget = false
			}
			if current == profile {
				inTarget = true
				continue
			}
		}
		if inTarget {
			continue
		}
		output = append(output, line)
	}
	if inTarget || !wrote {
		if len(output) > 0 && strings.TrimSpace(output[len(output)-1]) != "" {
			output = append(output, "")
		}
		output = append(output, newSection...)
	}

	content := strings.Join(output, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}

func splitLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func isSectionHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func parseSectionName(line string) string {
	return strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
}
