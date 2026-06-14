package chembl

import (
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network. The client's HTTP behaviour is
// covered in chembl_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "chembl" {
		t.Errorf("Scheme = %q, want chembl", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "chembl" {
		t.Errorf("Identity.Binary = %q, want chembl", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
		ok  bool
	}{
		{"CHEMBL25", "molecule", "CHEMBL25", true},
		{"chembl25", "molecule", "CHEMBL25", true},
		{"CHEMBL2035", "molecule", "CHEMBL2035", true},
		{"aspirin", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("Classify(%q) unexpected error: %v", tc.in, err)
				continue
			}
			if typ != tc.typ || id != tc.id {
				t.Errorf("Classify(%q) = (%q, %q), want (%q, %q)", tc.in, typ, id, tc.typ, tc.id)
			}
		} else {
			if err == nil {
				t.Errorf("Classify(%q) expected error, got (%q, %q)", tc.in, typ, id)
			}
		}
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		typ  string
		id   string
		want string
		ok   bool
	}{
		{"molecule", "CHEMBL25", "https://" + Host + "/chembl/compound_report_card/CHEMBL25/", true},
		{"target", "CHEMBL2035", "https://" + Host + "/chembl/target_report_card/CHEMBL2035/", true},
		{"activity", "1", "", false},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.typ, tc.id)
		if tc.ok {
			if err != nil {
				t.Errorf("Locate(%q, %q) unexpected error: %v", tc.typ, tc.id, err)
				continue
			}
			if got != tc.want {
				t.Errorf("Locate(%q, %q) = %q, want %q", tc.typ, tc.id, got, tc.want)
			}
		} else {
			if err == nil {
				t.Errorf("Locate(%q, %q) expected error, got %q", tc.typ, tc.id, got)
			}
		}
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	mol := &Molecule{ID: "CHEMBL25", Name: "ASPIRIN", SMILES: "CC(=O)Oc1ccccc1C(=O)O"}
	u, err := h.Mint(mol)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !strings.HasPrefix(u.String(), "chembl://") {
		t.Errorf("Mint = %q, want chembl:// prefix", u.String())
	}

	if body, ok := h.Body(mol); !ok || body == "" {
		t.Errorf("Body = (%q, %v), want non-empty", body, ok)
	}

	got, err := h.ResolveOn("chembl", "CHEMBL25")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if !strings.HasPrefix(got.String(), "chembl://molecule/") {
		t.Errorf("ResolveOn = %q, want chembl://molecule/ prefix", got.String())
	}
}
