package awssts

import (
	"errors"
	"regexp"
	"strings"
)

var runtimeAWSAccessKeyPattern = regexp.MustCompile(`^[A-Z0-9]{16,128}$`)

func validateRuntimeStaticCredentials(accessKeyID, secretAccessKey string) error {
	if !runtimeAWSAccessKeyPattern.MatchString(accessKeyID) {
		return errors.New("stored AWS access key ID is invalid; re-save the config with the correct key")
	}
	if strings.TrimSpace(secretAccessKey) != secretAccessKey {
		return errors.New("stored AWS secret access key has leading or trailing whitespace; re-save the config")
	}
	for _, r := range secretAccessKey {
		if r < 0x21 || r > 0x7e {
			return errors.New("stored AWS secret access key contains non-ASCII characters; re-save the config")
		}
	}
	return nil
}
