package awssso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/smithy-go"

	"github.com/yaman/aws-credential-manager/core-go/internal/metadata"
	"github.com/yaman/aws-credential-manager/core-go/internal/sessioncache"
)

const (
	authCodeGrantType       = "authorization_code"
	deviceGrantType         = "urn:ietf:params:oauth:grant-type:device_code"
	refreshGrantType        = "refresh_token"
	clientType              = "public"
	pkceChallengeMethod     = "S256"
	authorizationScope      = "sso:account:access"
	registeredRedirectURI   = "http://127.0.0.1/oauth/callback"
	callbackPath            = "/oauth/callback"
	authorizationWaitWindow = 10 * time.Minute
)

type SessionResult struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
	BrowserURL      string
}

type Service struct {
	cache *sessioncache.Store
}

type authCodeCallbackServer struct {
	listener net.Listener
	server   *http.Server
	resultCh chan authCodeResult
}

type authCodeResult struct {
	code  string
	state string
	err   string
}

func New(cache *sessioncache.Store) *Service {
	return &Service{cache: cache}
}

func (s *Service) Generate(ctx context.Context, input metadata.ConfigInput) (SessionResult, error) {
	if strings.TrimSpace(input.ID) == "" {
		return SessionResult{}, fmt.Errorf("config id is required")
	}
	if strings.TrimSpace(input.SSORegion) == "" || strings.TrimSpace(input.SSOStartURL) == "" || strings.TrimSpace(input.SSOAccountID) == "" || strings.TrimSpace(input.SSORoleName) == "" {
		return SessionResult{}, fmt.Errorf("ssoStartUrl, ssoRegion, ssoAccountId, and ssoRoleName are required")
	}

	startURL, err := normalizeStartURL(input.SSOStartURL)
	if err != nil {
		return SessionResult{}, err
	}
	input.SSOStartURL = startURL

	issuerURL, err := normalizeIssuerURL(input.SSOIssuerURL, input.SSOStartURL)
	if err != nil {
		return SessionResult{}, err
	}
	input.SSOIssuerURL = issuerURL

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(strings.TrimSpace(input.SSORegion)))
	if err != nil {
		return SessionResult{}, err
	}

	oidcClient := ssooidc.NewFromConfig(cfg)
	ssoClient := sso.NewFromConfig(cfg)

	session, _ := s.cache.Get(input.ID)
	if session.Registration.ClientID == "" || time.Now().After(session.Registration.ClientSecretExpiresAt) {
		registration, err := s.registerClient(ctx, oidcClient, input)
		if err != nil {
			return SessionResult{}, err
		}
		session.Registration = registration
	}

	accessToken, refreshToken, browserURL, accessExpiry, err := s.ensureAccessToken(ctx, oidcClient, input, session)
	if err != nil {
		return SessionResult{}, err
	}

	roleOut, err := ssoClient.GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccessToken: aws.String(accessToken),
		AccountId:   aws.String(strings.TrimSpace(input.SSOAccountID)),
		RoleName:    aws.String(strings.TrimSpace(input.SSORoleName)),
	})
	if err != nil {
		return SessionResult{}, err
	}
	if roleOut.RoleCredentials == nil {
		return SessionResult{}, fmt.Errorf("SSO returned empty role credentials")
	}

	expiration := time.UnixMilli(roleOut.RoleCredentials.Expiration)
	session.AccessToken = accessToken
	session.AccessExpiry = accessExpiry
	session.RefreshToken = refreshToken
	session.LastBrowserURL = browserURL
	s.cache.Put(input.ID, session)

	return SessionResult{
		AccessKeyID:     aws.ToString(roleOut.RoleCredentials.AccessKeyId),
		SecretAccessKey: aws.ToString(roleOut.RoleCredentials.SecretAccessKey),
		SessionToken:    aws.ToString(roleOut.RoleCredentials.SessionToken),
		Expiration:      expiration,
		BrowserURL:      browserURL,
	}, nil
}

func (s *Service) registerClient(ctx context.Context, client *ssooidc.Client, input metadata.ConfigInput) (sessioncache.Registration, error) {
	out, err := client.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName:   aws.String("aws-credential-manager"),
		ClientType:   aws.String(clientType),
		GrantTypes:   []string{authCodeGrantType, refreshGrantType, deviceGrantType},
		RedirectUris: []string{registeredRedirectURI},
		IssuerUrl:    aws.String(strings.TrimSpace(input.SSOIssuerURL)),
		Scopes:       []string{authorizationScope},
	})
	if err != nil {
		return sessioncache.Registration{}, explainRegisterClientError(err, input.SSOIssuerURL, input.SSORegion)
	}
	return sessioncache.Registration{
		ClientID:              aws.ToString(out.ClientId),
		ClientSecret:          aws.ToString(out.ClientSecret),
		ClientSecretExpiresAt: time.Unix(out.ClientSecretExpiresAt, 0),
	}, nil
}

func (s *Service) ensureAccessToken(ctx context.Context, client *ssooidc.Client, input metadata.ConfigInput, session sessioncache.Session) (string, string, string, time.Time, error) {
	if session.AccessToken != "" && time.Now().Add(1*time.Minute).Before(session.AccessExpiry) {
		return session.AccessToken, session.RefreshToken, session.LastBrowserURL, session.AccessExpiry, nil
	}

	if session.RefreshToken != "" {
		out, err := client.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     aws.String(session.Registration.ClientID),
			ClientSecret: aws.String(session.Registration.ClientSecret),
			GrantType:    aws.String(refreshGrantType),
			RefreshToken: aws.String(session.RefreshToken),
		})
		if err == nil {
			refreshToken := session.RefreshToken
			if out.RefreshToken != nil && *out.RefreshToken != "" {
				refreshToken = *out.RefreshToken
			}
			expiry := time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
			return aws.ToString(out.AccessToken), refreshToken, session.LastBrowserURL, expiry, nil
		}
	}

	return s.authorizationCodeFlow(ctx, client, input, session.Registration)
}

func (s *Service) authorizationCodeFlow(ctx context.Context, client *ssooidc.Client, input metadata.ConfigInput, registration sessioncache.Registration) (string, string, string, time.Time, error) {
	callbackServer, err := newAuthCodeCallbackServer()
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	defer callbackServer.Close()

	codeVerifier, err := randomURLSafeString(64)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	state, err := randomURLSafeString(32)
	if err != nil {
		return "", "", "", time.Time{}, err
	}

	browserURL := buildAuthorizationURL(strings.TrimSpace(input.SSORegion), registration.ClientID, callbackServer.RedirectURI(), state, codeVerifier)
	if browserURL == "" {
		return "", "", "", time.Time{}, fmt.Errorf("failed to build SSO authorization URL")
	}
	if err := exec.Command("open", browserURL).Run(); err != nil {
		return "", "", browserURL, time.Time{}, fmt.Errorf("failed to open browser for SSO login: %w", err)
	}

	callbackCtx, cancel := context.WithTimeout(ctx, authorizationWaitWindow)
	defer cancel()

	code, returnedState, err := callbackServer.WaitForCallback(callbackCtx)
	if err != nil {
		return "", "", browserURL, time.Time{}, err
	}
	if returnedState != state {
		return "", "", browserURL, time.Time{}, fmt.Errorf("SSO callback state mismatch")
	}

	tokenOut, err := client.CreateToken(ctx, &ssooidc.CreateTokenInput{
		ClientId:     aws.String(registration.ClientID),
		ClientSecret: aws.String(registration.ClientSecret),
		GrantType:    aws.String(authCodeGrantType),
		Code:         aws.String(code),
		CodeVerifier: aws.String(codeVerifier),
		RedirectUri:  aws.String(callbackServer.RedirectURI()),
	})
	if err != nil {
		return "", "", browserURL, time.Time{}, fmt.Errorf("SSO token exchange failed: %w", err)
	}

	expiry := time.Now().Add(time.Duration(tokenOut.ExpiresIn) * time.Second)
	return aws.ToString(tokenOut.AccessToken), aws.ToString(tokenOut.RefreshToken), browserURL, expiry, nil
}

func buildAuthorizationURL(region, clientID, redirectURI, state, codeVerifier string) string {
	if strings.TrimSpace(region) == "" || strings.TrimSpace(clientID) == "" || strings.TrimSpace(redirectURI) == "" {
		return ""
	}

	challenge := sha256.Sum256([]byte(codeVerifier))
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	params.Set("scope", authorizationScope)
	params.Set("code_challenge_method", pkceChallengeMethod)
	params.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))

	return fmt.Sprintf("https://oidc.%s.amazonaws.com/authorize?%s", strings.TrimSpace(region), params.Encode())
}

func explainRegisterClientError(err error, issuerURL, region string) error {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.ErrorCode() != "InvalidRequestException" {
		return err
	}

	return fmt.Errorf(
		"invalid SSO issuer URL %q or SSO region %q; in IAM Identity Center, use the Issuer URL for the same region as the instance: %w",
		strings.TrimSpace(issuerURL),
		strings.TrimSpace(region),
		err,
	)
}

func normalizeStartURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("ssoStartUrl is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid SSO Start URL %q: %w", trimmed, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf(
			"invalid SSO Start URL %q; use the AWS access portal URL from IAM Identity Center Settings, for example https://<tenant>.awsapps.com/start",
			trimmed,
		)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Path != "/" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}

	return parsed.String(), nil
}

func normalizeIssuerURL(rawIssuerURL, fallbackStartURL string) (string, error) {
	trimmed := strings.TrimSpace(rawIssuerURL)
	if trimmed == "" {
		trimmed = strings.TrimSpace(fallbackStartURL)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid SSO Issuer URL %q: %w", trimmed, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf(
			"invalid SSO Issuer URL %q; use the Issuer URL from IAM Identity Center Settings",
			trimmed,
		)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Path != "/" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}

	return parsed.String(), nil
}

func newAuthCodeCallbackServer() (*authCodeCallbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local callback listener: %w", err)
	}

	resultCh := make(chan authCodeResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
	}
	callbackServer := &authCodeCallbackServer{
		listener: listener,
		server:   server,
		resultCh: resultCh,
	}

	mux.HandleFunc(callbackPath, callbackServer.handleCallback)
	go func() {
		_ = server.Serve(listener)
	}()

	return callbackServer, nil
}

func (s *authCodeCallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result := authCodeResult{
		code:  query.Get("code"),
		state: query.Get("state"),
		err:   query.Get("error"),
	}
	select {
	case s.resultCh <- result:
	default:
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if result.err != "" {
		_, _ = fmt.Fprintf(w, "<html><body><h3>SSO login failed</h3><p>%s</p></body></html>", html.EscapeString(result.err))
		return
	}
	_, _ = w.Write([]byte("<html><body><h3>SSO login completed</h3><p>You can return to AWS Credential Manager.</p></body></html>"))
}

func (s *authCodeCallbackServer) RedirectURI() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", s.listener.Addr().(*net.TCPAddr).Port, callbackPath)
}

func (s *authCodeCallbackServer) WaitForCallback(ctx context.Context) (string, string, error) {
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return "", "", fmt.Errorf("SSO browser login cancelled")
			}
			return "", "", fmt.Errorf("SSO browser login timed out")
		case result := <-s.resultCh:
			if result.err != "" {
				return "", "", fmt.Errorf("SSO authorization failed: %s", result.err)
			}
			if strings.TrimSpace(result.code) == "" {
				continue
			}
			return result.code, result.state, nil
		}
	}
}

func (s *authCodeCallbackServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

func randomURLSafeString(size int) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("size must be positive")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
