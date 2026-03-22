package awssts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"

	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
)

type SessionResult struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
}

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Generate(ctx context.Context, input metadata.ConfigInput) (SessionResult, error) {
	accessKeyID := strings.TrimSpace(input.AWSAccessKeyID)
	secretAccessKey := input.AWSSecretAccessKey
	if err := validateRuntimeStaticCredentials(accessKeyID, secretAccessKey); err != nil {
		return SessionResult{}, err
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKeyID,
			strings.TrimSpace(secretAccessKey),
			"",
		)),
		awsconfig.WithRegion(defaultRegion(input.STSRegion)),
	)
	if err != nil {
		return SessionResult{}, err
	}

	client := sts.NewFromConfig(cfg)
	tokenCode := strings.TrimSpace(input.MFATOTP)
	if tokenCode == "" {
		return SessionResult{}, fmt.Errorf("MFA TOTP code is required")
	}

	roleArn := strings.TrimSpace(input.RoleArn)
	if roleArn != "" && !strings.HasPrefix(roleArn, "arn:") {
		return SessionResult{}, fmt.Errorf("stored role ARN is invalid; clear Role ARN for GetSessionToken or set a valid AWS role ARN")
	}

	if roleArn == "" {
		duration, err := secondsPtr(input.SessionDuration)
		if err != nil {
			return SessionResult{}, err
		}
		out, err := client.GetSessionToken(ctx, &sts.GetSessionTokenInput{
			DurationSeconds: duration,
			SerialNumber:    stringPtr(strings.TrimSpace(input.MFAArn)),
			TokenCode:       stringPtr(tokenCode),
		})
		if err != nil {
			return SessionResult{}, err
		}
		if out.Credentials == nil {
			return SessionResult{}, fmt.Errorf("STS returned empty credentials")
		}
		return mapResult(out.Credentials), nil
	}

	duration, err := secondsPtr(input.SessionDuration)
	if err != nil {
		return SessionResult{}, err
	}
	roleSessionName := strings.TrimSpace(input.RoleSessionName)
	if roleSessionName == "" {
		roleSessionName = fmt.Sprintf("aws-credential-manager-%d", time.Now().Unix())
	}
	out, err := client.AssumeRole(ctx, &sts.AssumeRoleInput{
		DurationSeconds: duration,
		ExternalId:      optionalString(strings.TrimSpace(input.ExternalID)),
		RoleArn:         stringPtr(roleArn),
		RoleSessionName: stringPtr(roleSessionName),
		SerialNumber:    stringPtr(strings.TrimSpace(input.MFAArn)),
		TokenCode:       stringPtr(tokenCode),
	})
	if err != nil {
		return SessionResult{}, err
	}
	if out.Credentials == nil {
		return SessionResult{}, fmt.Errorf("STS returned empty credentials")
	}
	return mapResult(out.Credentials), nil
}

func mapResult(creds *ststypes.Credentials) SessionResult {
	return SessionResult{
		AccessKeyID:     value(creds.AccessKeyId),
		SecretAccessKey: value(creds.SecretAccessKey),
		SessionToken:    value(creds.SessionToken),
		Expiration:      valueTime(creds.Expiration),
	}
}

func defaultRegion(region string) string {
	if strings.TrimSpace(region) == "" {
		return "us-east-1"
	}
	return strings.TrimSpace(region)
}

func secondsPtr(durationMinutes string) (*int32, error) {
	if strings.TrimSpace(durationMinutes) == "" {
		return nil, nil
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(durationMinutes))
	if err != nil {
		return nil, fmt.Errorf("invalid session duration: %w", err)
	}
	seconds := int32(minutes * 60)
	return &seconds, nil
}

func stringPtr(value string) *string {
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func valueTime(ptr *time.Time) time.Time {
	if ptr == nil {
		return time.Time{}
	}
	return *ptr
}
