// Package chembl is the library behind the chembl command line:
// the HTTP client, request shaping, and the typed data models for the
// ChEMBL REST API (https://www.ebi.ac.uk/chembl/api/data).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package chembl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultUserAgent identifies the client to ChEMBL.
const DefaultUserAgent = "chembl-cli/0.1.0 (github.com/tamnd/chembl-cli)"

// Host is the EBI ChEMBL web host.
const Host = "www.ebi.ac.uk"

// BaseURL is the root every request is built from.
const BaseURL = "https://www.ebi.ac.uk/chembl/api/data"

// Config holds the tunable knobs for a Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns sensible defaults for calling the ChEMBL API.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Timeout:   30 * time.Second,
		Retries:   3,
	}
}

// Client talks to the ChEMBL REST API over HTTP.
type Client struct {
	HTTP    *http.Client
	cfg     Config
	mu      sync.Mutex
	last    time.Time
}

// NewClientWithConfig returns a Client built from the provided Config.
func NewClientWithConfig(cfg Config) *Client {
	return &Client{
		HTTP: &http.Client{Timeout: cfg.Timeout},
		cfg:  cfg,
	}
}

// NewClient returns a Client with DefaultConfig.
func NewClient() *Client {
	return NewClientWithConfig(DefaultConfig())
}

// SetRetries updates the number of retries (useful in tests).
func (c *Client) SetRetries(n int) {
	c.cfg.Retries = n
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's config. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// ---------------------------------------------------------------------------
// Data models
// ---------------------------------------------------------------------------

// MoleculeProps holds physico-chemical properties for a molecule.
type MoleculeProps struct {
	Formula   string `json:"full_molecular_formula"`
	MolWeight string `json:"full_mwt"`
}

// MoleculeStructs holds structural representations.
type MoleculeStructs struct {
	SMILES string `json:"canonical_smiles"`
}

// molecule is the raw API shape. Fields from nested objects are promoted into
// the flat Molecule output type below.
type molecule struct {
	ID       string           `json:"molecule_chembl_id"`
	Name     string           `json:"pref_name"`
	MaxPhase int              `json:"max_phase"`
	Type     string           `json:"molecule_type"`
	Props    *MoleculeProps   `json:"molecule_properties"`
	Structs  *MoleculeStructs `json:"molecule_structures"`
}

// Molecule is the flat output record for a ChEMBL molecule.
type Molecule struct {
	ID        string `json:"molecule_chembl_id" kit:"id"`
	Name      string `json:"pref_name"`
	MaxPhase  int    `json:"max_phase"`
	Type      string `json:"molecule_type"`
	Formula   string `json:"formula,omitempty"`
	MolWeight string `json:"mol_weight,omitempty"`
	SMILES    string `json:"smiles,omitempty" kit:"body"`
}

func flatMolecule(m *molecule) *Molecule {
	out := &Molecule{
		ID:       m.ID,
		Name:     m.Name,
		MaxPhase: m.MaxPhase,
		Type:     m.Type,
	}
	if m.Props != nil {
		out.Formula = m.Props.Formula
		out.MolWeight = m.Props.MolWeight
	}
	if m.Structs != nil {
		out.SMILES = m.Structs.SMILES
	}
	return out
}

// Target is a ChEMBL biological target.
type Target struct {
	ID       string `json:"target_chembl_id" kit:"id"`
	Name     string `json:"pref_name" kit:"body"`
	Type     string `json:"target_type"`
	Organism string `json:"organism"`
	TaxID    int    `json:"tax_id,omitempty"`
}

// Activity is a single bioactivity measurement from ChEMBL.
type Activity struct {
	ActivityID   int    `json:"activity_id" kit:"id"`
	AssayID      string `json:"assay_chembl_id"`
	MoleculeID   string `json:"molecule_chembl_id"`
	TargetID     string `json:"target_chembl_id"`
	TargetName   string `json:"target_pref_name" kit:"body"`
	ActivityType string `json:"activity_type"`
	Value        string `json:"value"`
	Units        string `json:"units"`
}

// ---------------------------------------------------------------------------
// API helpers
// ---------------------------------------------------------------------------

type moleculeListResp struct {
	Molecules []*molecule `json:"molecules"`
	PageMeta  PageMeta    `json:"page_meta"`
}

type targetListResp struct {
	Targets  []*Target `json:"targets"`
	PageMeta PageMeta  `json:"page_meta"`
}

type activityListResp struct {
	Activities []*Activity `json:"activities"`
	PageMeta   PageMeta    `json:"page_meta"`
}

// PageMeta holds pagination metadata from a list response.
type PageMeta struct {
	TotalCount int `json:"total_count"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
}

// Molecule fetches a single molecule by its ChEMBL ID (e.g. "CHEMBL25").
func (c *Client) Molecule(ctx context.Context, id string) (*Molecule, error) {
	u := c.cfg.BaseURL + "/molecule/" + url.PathEscape(id) + ".json"
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("molecule %s: %w", id, err)
	}
	var m molecule
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("molecule %s: decode: %w", id, err)
	}
	return flatMolecule(&m), nil
}

// SearchMolecules searches molecules by name or SMILES query.
func (c *Client) SearchMolecules(ctx context.Context, q string, limit int) ([]*Molecule, error) {
	if limit <= 0 {
		limit = 20
	}
	u := fmt.Sprintf("%s/molecule/search.json?q=%s&limit=%d",
		c.cfg.BaseURL, url.QueryEscape(q), limit)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("search molecules %q: %w", q, err)
	}
	var resp moleculeListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("search molecules %q: decode: %w", q, err)
	}
	out := make([]*Molecule, 0, len(resp.Molecules))
	for _, m := range resp.Molecules {
		out = append(out, flatMolecule(m))
	}
	return out, nil
}

// Target fetches a single target by its ChEMBL ID.
func (c *Client) Target(ctx context.Context, id string) (*Target, error) {
	u := c.cfg.BaseURL + "/target/" + url.PathEscape(id) + ".json"
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("target %s: %w", id, err)
	}
	var t Target
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("target %s: decode: %w", id, err)
	}
	return &t, nil
}

// SearchTargets searches targets by name query.
func (c *Client) SearchTargets(ctx context.Context, q string, limit int) ([]*Target, error) {
	if limit <= 0 {
		limit = 20
	}
	u := fmt.Sprintf("%s/target/search.json?q=%s&limit=%d",
		c.cfg.BaseURL, url.QueryEscape(q), limit)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("search targets %q: %w", q, err)
	}
	var resp targetListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("search targets %q: decode: %w", q, err)
	}
	return resp.Targets, nil
}

// Activities fetches bioactivities filtered by molecule or target ChEMBL ID.
// At least one of moleculeID or targetID must be non-empty.
func (c *Client) Activities(ctx context.Context, moleculeID, targetID string, limit int) ([]*Activity, error) {
	if moleculeID == "" && targetID == "" {
		return nil, fmt.Errorf("activities: at least one of molecule or target ID required")
	}
	if limit <= 0 {
		limit = 20
	}
	params := url.Values{}
	if moleculeID != "" {
		params.Set("molecule_chembl_id", moleculeID)
	}
	if targetID != "" {
		params.Set("target_chembl_id", targetID)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))
	u := c.cfg.BaseURL + "/activity.json?" + params.Encode()
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("activities: %w", err)
	}
	var resp activityListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("activities: decode: %w", err)
	}
	return resp.Activities, nil
}
