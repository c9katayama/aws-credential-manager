package generator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yaman/aws-credential-manager/core-go/internal/awssso"
	"github.com/yaman/aws-credential-manager/core-go/internal/awssts"
	"github.com/yaman/aws-credential-manager/core-go/internal/credentialsfile"
	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
	onepasswordmanager "github.com/yaman/aws-credential-manager/core-go/internal/onepassword"
)

type Result struct {
	ConfigID        string                 `json:"configId"`
	ProfileName     string                 `json:"profileName"`
	AuthType        string                 `json:"authType"`
	Expiration      time.Time              `json:"expiration"`
	LastRefreshTime time.Time              `json:"lastRefreshTime"`
	BrowserURL      string                 `json:"browserUrl,omitempty"`
	Summary         metadata.ConfigSummary `json:"summary"`
}

type configLoader interface {
	LoadRuntimeConfig(ctx context.Context, summary metadata.ConfigSummary) (metadata.ConfigInput, error)
}

type credentialsWriter interface {
	UpsertProfile(profile string, creds credentialsfile.SessionCredentials) error
}

type stsGenerator interface {
	Generate(ctx context.Context, input metadata.ConfigInput) (awssts.SessionResult, error)
}

type ssoGenerator interface {
	Generate(ctx context.Context, input metadata.ConfigInput) (awssso.SessionResult, error)
}

type Service struct {
	opManager        configLoader
	metadataStore    *metadata.Store
	credentialsStore credentialsWriter
	stsService       stsGenerator
	ssoService       ssoGenerator
	mu               sync.Mutex
	inFlight         map[string]bool
}

func New(opManager *onepasswordmanager.Manager, metadataStore *metadata.Store, credentialsStore *credentialsfile.Store, stsService *awssts.Service, ssoService *awssso.Service) *Service {
	return &Service{
		opManager:        opManager,
		metadataStore:    metadataStore,
		credentialsStore: credentialsStore,
		stsService:       stsService,
		ssoService:       ssoService,
		inFlight:         map[string]bool{},
	}
}

func (s *Service) Generate(ctx context.Context, id string) (Result, error) {
	if err := s.begin(id); err != nil {
		return Result{}, err
	}
	defer s.finish(id)

	summary, err := s.metadataStore.Get(id)
	if err != nil {
		return Result{}, err
	}

	input, err := s.opManager.LoadRuntimeConfig(ctx, summary)
	if err != nil {
		return Result{}, err
	}

	var accessKeyID, secretKey, token, browserURL string
	var expiration time.Time
	switch input.AuthType {
	case "sts":
		res, err := s.stsService.Generate(ctx, input)
		if err != nil {
			s.recordError(id, err)
			return Result{}, err
		}
		accessKeyID = res.AccessKeyID
		secretKey = res.SecretAccessKey
		token = res.SessionToken
		expiration = res.Expiration
	case "sso":
		res, err := s.ssoService.Generate(ctx, input)
		if err != nil {
			s.recordError(id, err)
			return Result{}, err
		}
		accessKeyID = res.AccessKeyID
		secretKey = res.SecretAccessKey
		token = res.SessionToken
		expiration = res.Expiration
		browserURL = res.BrowserURL
	default:
		err := fmt.Errorf("unsupported auth type: %s", input.AuthType)
		s.recordError(id, err)
		return Result{}, err
	}

	if err := s.credentialsStore.UpsertProfile(summary.ProfileName, credentialsfile.SessionCredentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretKey,
		SessionToken:    token,
	}); err != nil {
		s.recordError(id, err)
		return Result{}, err
	}

	updatedSummary, err := s.metadataStore.RecordResult(id, &expiration, "")
	if err != nil {
		return Result{}, err
	}

	lastRefresh := time.Now().UTC()
	if updatedSummary.LastRefreshTime != nil {
		lastRefresh = *updatedSummary.LastRefreshTime
	}
	return Result{
		ConfigID:        id,
		ProfileName:     summary.ProfileName,
		AuthType:        summary.AuthType,
		Expiration:      expiration,
		LastRefreshTime: lastRefresh,
		BrowserURL:      browserURL,
		Summary:         updatedSummary,
	}, nil
}

func (s *Service) recordError(id string, err error) {
	_, _ = s.metadataStore.RecordResult(id, nil, err.Error())
}

func (s *Service) begin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight == nil {
		s.inFlight = map[string]bool{}
	}
	if s.inFlight[id] {
		return fmt.Errorf("generation already in progress")
	}
	s.inFlight[id] = true
	return nil
}

func (s *Service) finish(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, id)
}
