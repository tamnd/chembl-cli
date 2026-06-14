package chembl_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tamnd/chembl-cli/chembl"
)

// newTestClient builds a Client pointed at the given test server URL with no pacing.
func newTestClient(baseURL string) *chembl.Client {
	cfg := chembl.DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.Rate = 0
	return chembl.NewClientWithConfig(cfg)
}

// ---------------------------------------------------------------------------
// Molecule GET
// ---------------------------------------------------------------------------

func TestMolecule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/molecule/CHEMBL25.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"molecule_chembl_id": "CHEMBL25",
			"pref_name":          "ASPIRIN",
			"max_phase":          4,
			"molecule_type":      "Small molecule",
			"molecule_properties": map[string]any{
				"full_molformula": "C9H8O4",
				"full_mwt":        "180.16",
			},
			"molecule_structures": map[string]any{
				"canonical_smiles": "CC(=O)Oc1ccccc1C(=O)O",
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	mol, err := c.Molecule(context.Background(), "CHEMBL25")
	if err != nil {
		t.Fatal(err)
	}
	if mol.ID != "CHEMBL25" {
		t.Errorf("ID = %q, want CHEMBL25", mol.ID)
	}
	if mol.Name != "ASPIRIN" {
		t.Errorf("Name = %q, want ASPIRIN", mol.Name)
	}
	if mol.Formula != "C9H8O4" {
		t.Errorf("Formula = %q, want C9H8O4", mol.Formula)
	}
	if mol.MolWeight != "180.16" {
		t.Errorf("MolWeight = %q, want 180.16", mol.MolWeight)
	}
	if mol.SMILES == "" {
		t.Error("SMILES is empty")
	}
}

// ---------------------------------------------------------------------------
// Molecule search
// ---------------------------------------------------------------------------

func TestSearchMolecules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/molecule/search.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"molecules": []any{
				map[string]any{
					"molecule_chembl_id": "CHEMBL25",
					"pref_name":          "ASPIRIN",
					"max_phase":          4,
					"molecule_type":      "Small molecule",
				},
			},
			"page_meta": map[string]any{"total_count": 1, "limit": 20, "offset": 0},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.SearchMolecules(context.Background(), "aspirin", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Name != "ASPIRIN" {
		t.Errorf("Name = %q, want ASPIRIN", results[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Target search
// ---------------------------------------------------------------------------

func TestSearchTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/target/search.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"targets": []any{
				map[string]any{
					"target_chembl_id": "CHEMBL2035",
					"pref_name":        "Kinase",
					"target_type":      "SINGLE PROTEIN",
					"organism":         "Homo sapiens",
					"tax_id":           9606,
				},
			},
			"page_meta": map[string]any{"total_count": 1723, "limit": 2, "offset": 0},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.SearchTargets(context.Background(), "kinase", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Name != "Kinase" {
		t.Errorf("Name = %q, want Kinase", results[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Activity by molecule
// ---------------------------------------------------------------------------

func TestActivitiesByMolecule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/activity.json" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("molecule_chembl_id") != "CHEMBL25" {
			http.Error(w, "bad param", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"activities": []any{
				map[string]any{
					"activity_id":        1,
					"assay_chembl_id":    "CHEMBL123",
					"molecule_chembl_id": "CHEMBL25",
					"target_chembl_id":   "CHEMBL2035",
					"target_pref_name":   "Cyclooxygenase",
					"activity_type":      "IC50",
					"value":              "0.5",
					"units":              "uM",
				},
			},
			"page_meta": map[string]any{"total_count": 4087, "limit": 2, "offset": 0},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	acts, err := c.Activities(context.Background(), "CHEMBL25", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 {
		t.Fatalf("len(acts) = %d, want 1", len(acts))
	}
	if acts[0].ActivityType != "IC50" {
		t.Errorf("ActivityType = %q, want IC50", acts[0].ActivityType)
	}
}

// ---------------------------------------------------------------------------
// 503 retry
// ---------------------------------------------------------------------------

func TestRetryOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"molecule_chembl_id": "CHEMBL25",
			"pref_name":          "ASPIRIN",
			"max_phase":          4,
			"molecule_type":      "Small molecule",
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetRetries(5)

	mol, err := c.Molecule(context.Background(), "CHEMBL25")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if mol.Name != "ASPIRIN" {
		t.Errorf("Name = %q, want ASPIRIN", mol.Name)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}

// ---------------------------------------------------------------------------
// No params for Activities
// ---------------------------------------------------------------------------

func TestActivitiesRequiresParams(t *testing.T) {
	c := newTestClient("http://localhost")
	_, err := c.Activities(context.Background(), "", "", 5)
	if err == nil {
		t.Error("expected error when no molecule or target ID given")
	}
}
