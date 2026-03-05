package client

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is an authenticated HTTP client for the ac12.dev platform API.
type Client struct {
	BaseURL    string
	Username   string
	privateKey ed25519.PrivateKey
	httpClient *http.Client
}

// Project represents a platform project.
type Project struct {
	ID           string `json:"id"`
	OwnerID      string `json:"owner_id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	IsDefault    bool   `json:"is_default"`
	DatabasePath string `json:"database_path"`
	ServiceCount int    `json:"service_count"`
	CreatedAt    string `json:"created_at"`
}

// Service represents a deployed container service.
type Service struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Env       map[string]string `json:"env"`
	Port      *int64            `json:"port"`
	Daemon    bool              `json:"daemon"`
	Status    string            `json:"status"`
	URL       string            `json:"url"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// Domain represents a subdomain mapping.
type Domain struct {
	ID                   string `json:"id"`
	ProjectID            string `json:"project_id"`
	Subdomain            string `json:"subdomain"`
	CustomDomain         string `json:"custom_domain"`
	CustomDomainVerified bool   `json:"custom_domain_verified"`
	TargetType           string `json:"target_type"`
	TargetService        string `json:"target_service"`
	TargetPath           string `json:"target_path"`
	URL                  string `json:"url"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// CronJob represents a scheduled HTTP task.
type CronJob struct {
	ID            string      `json:"id"`
	ProjectID     string      `json:"project_id"`
	Name          string      `json:"name"`
	Schedule      string      `json:"schedule"`
	TargetType    string      `json:"target_type"`
	TargetService string      `json:"target_service"`
	TargetPath    string      `json:"target_path"`
	HTTPMethod    string      `json:"http_method"`
	HTTPBody      string      `json:"http_body"`
	Enabled       bool        `json:"enabled"`
	LastRunAt     string      `json:"last_run_at"`
	LastStatus    interface{} `json:"last_status"` // API returns int HTTP status code or null
	CreatedAt     string      `json:"created_at"`
	UpdatedAt     string      `json:"updated_at"`
}

// File represents a stored project file.
type File struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	IsPublic    bool   `json:"is_public"`
	URL         string `json:"url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Secret represents a project secret (value is never returned by the API).
type Secret struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// EmailAccount represents an @ac12.dev mailbox.
type EmailAccount struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// Agent represents an AI coding agent attached to a project.
type Agent struct {
	ID               string   `json:"id"`
	ProjectID        string   `json:"project_id"`
	Name             string   `json:"name"`
	AgentType        string   `json:"agent_type"`
	Model            string   `json:"model"`
	SystemPrompt     string   `json:"system_prompt"`
	IdentityUsername string   `json:"identity_username"`
	Skills           []string `json:"skills"`
	EffectiveSkills  []string `json:"effective_skills"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

// IAMRole represents a named permission set.
type IAMRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Operations  string `json:"operations"`
	Resource    string `json:"resource"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// IAMGroupMember represents a member inside a group.
type IAMGroupMember struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	AddedAt  string `json:"added_at"`
}

// IAMGroup represents a named collection of roles and members.
type IAMGroup struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	IsPersonal  bool             `json:"is_personal"`
	OwnerID     string           `json:"owner_id"`
	Roles       []IAMRole        `json:"roles"`
	Members     []IAMGroupMember `json:"members"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
}

// New creates a new authenticated client.
func New(baseURL, username, privateKeyPEM string) (*Client, error) {
	pk, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return &Client{
		BaseURL:    baseURL,
		Username:   username,
		privateKey: pk,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func parsePrivateKey(pemStr string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	edKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not Ed25519")
	}
	return edKey, nil
}

// signPath extracts just the path component (no query string) for signing,
// matching the server's verification logic.
func signPath(apiPath string) string {
	if idx := strings.Index(apiPath, "?"); idx != -1 {
		return apiPath[:idx]
	}
	return apiPath
}

func (c *Client) sign(method, apiPath string, body []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := sha256.Sum256(body)
	// Server verifies against the bare path, not the path+query string
	msg := fmt.Sprintf("%s\n%s\n%s\n%x", method, signPath(apiPath), ts, h)
	sig := ed25519.Sign(c.privateKey, []byte(msg))
	return map[string]string{
		"X-Username":  c.Username,
		"X-Timestamp": ts,
		"X-Signature": base64.StdEncoding.EncodeToString(sig),
	}
}

// Do performs an authenticated JSON request. Returns the response body and HTTP status code.
func (c *Client) Do(method, apiPath string, bodyJSON interface{}) ([]byte, int, error) {
	var body []byte
	var err error

	if bodyJSON != nil {
		body, err = json.Marshal(bodyJSON)
		if err != nil {
			return nil, 0, err
		}
	}
	if body == nil {
		body = []byte{}
	}

	u, err := url.Parse(c.BaseURL + apiPath)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}

	if bodyJSON != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range c.sign(method, apiPath, body) {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// Upload performs an authenticated raw-body upload (for binary files).
func (c *Client) Upload(apiPath string, data []byte, contentType string, queryParams map[string]string) ([]byte, int, error) {
	u, err := url.Parse(c.BaseURL + apiPath)
	if err != nil {
		return nil, 0, err
	}

	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for k, v := range c.sign("POST", apiPath, data) {
		req.Header.Set(k, v)
	}

	uploadClient := &http.Client{Timeout: 10 * time.Minute}
	resp, err := uploadClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// DecodeJSON unmarshals response bytes into a target struct.
func DecodeJSON[T any](data []byte) (*T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w\nbody: %s", err, string(data))
	}
	return &v, nil
}

// APIError formats an HTTP error response into a readable error.
func APIError(statusCode int, body []byte) error {
	var errObj map[string]interface{}
	if err := json.Unmarshal(body, &errObj); err == nil {
		if detail, ok := errObj["detail"]; ok {
			return fmt.Errorf("HTTP %d: %v", statusCode, detail)
		}
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, string(body))
}
