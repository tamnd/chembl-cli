package chembl

import (
	"context"
	"fmt"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes chembl as a kit Domain. A multi-domain host (ant) enables
// it with a single blank import:
//
//	import _ "github.com/tamnd/chembl-cli/chembl"
//
// The init below registers it; the host then dereferences chembl:// URIs by
// routing to the operations Register installs. The same Domain also builds the
// standalone chembl binary (see cli.NewApp), so the binary and a host share
// one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the ChEMBL driver.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "chembl",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "chembl",
			Short:  "CLI for the ChEMBL drug and bioactivity database",
			Long: `CLI for the ChEMBL drug and bioactivity database

chembl reads public ChEMBL data via the EBI REST API, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/chembl-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// molecule: get a single molecule by ChEMBL ID.
	kit.Handle(app, kit.OpMeta{Name: "molecule", Group: "read", Single: true,
		Summary: "Get a molecule by ChEMBL ID (e.g. CHEMBL25)", URIType: "molecule", Resolver: true,
		Args: []kit.Arg{{Name: "id", Help: "ChEMBL molecule ID"}}}, getMolecule)

	// search: search molecules or targets.
	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search molecules or targets by name or SMILES",
		Args:    []kit.Arg{{Name: "query", Help: "search query"}}}, doSearch)

	// target: get a single target by ChEMBL ID.
	kit.Handle(app, kit.OpMeta{Name: "target", Group: "read", Single: true,
		Summary: "Get a target by ChEMBL ID", URIType: "target", Resolver: true,
		Args: []kit.Arg{{Name: "id", Help: "ChEMBL target ID"}}}, getTarget)

	// activity: list bioactivities filtered by molecule or target.
	kit.Handle(app, kit.OpMeta{Name: "activity", Group: "read", List: true,
		Summary: "List bioactivities for a molecule or target"}, getActivities)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClientWithConfig(c), nil
}

// ---------------------------------------------------------------------------
// Input structs
// ---------------------------------------------------------------------------

type moleculeIn struct {
	ID     string  `kit:"arg" help:"ChEMBL molecule ID (e.g. CHEMBL25)"`
	Client *Client `kit:"inject"`
}

type searchIn struct {
	Query  string  `kit:"arg" help:"search query (name or SMILES)"`
	Limit  int     `kit:"flag,inherit" help:"max results (default 20)"`
	Type   string  `kit:"flag" help:"resource type: molecule or target (default: molecule)"`
	Client *Client `kit:"inject"`
}

type targetIn struct {
	ID     string  `kit:"arg" help:"ChEMBL target ID (e.g. CHEMBL2035)"`
	Client *Client `kit:"inject"`
}

type activityIn struct {
	Molecule string  `kit:"flag" help:"filter by molecule ChEMBL ID"`
	Target   string  `kit:"flag" help:"filter by target ChEMBL ID"`
	Limit    int     `kit:"flag,inherit" help:"max results (default 20)"`
	Client   *Client `kit:"inject"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func getMolecule(ctx context.Context, in moleculeIn, emit func(*Molecule) error) error {
	mol, err := in.Client.Molecule(ctx, in.ID)
	if err != nil {
		return mapErr(err)
	}
	return emit(mol)
}

func doSearch(ctx context.Context, in searchIn, emit func(any) error) error {
	rtype := strings.ToLower(strings.TrimSpace(in.Type))
	if rtype == "" {
		rtype = "molecule"
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	switch rtype {
	case "molecule":
		results, err := in.Client.SearchMolecules(ctx, in.Query, limit)
		if err != nil {
			return mapErr(err)
		}
		for _, m := range results {
			if err := emit(m); err != nil {
				return err
			}
		}
	case "target":
		results, err := in.Client.SearchTargets(ctx, in.Query, limit)
		if err != nil {
			return mapErr(err)
		}
		for _, t := range results {
			if err := emit(t); err != nil {
				return err
			}
		}
	default:
		return errs.Usage("unknown type %q: use molecule or target", rtype)
	}
	return nil
}

func getTarget(ctx context.Context, in targetIn, emit func(*Target) error) error {
	t, err := in.Client.Target(ctx, in.ID)
	if err != nil {
		return mapErr(err)
	}
	return emit(t)
}

func getActivities(ctx context.Context, in activityIn, emit func(*Activity) error) error {
	if in.Molecule == "" && in.Target == "" {
		return errs.Usage("at least one of --molecule or --target is required")
	}
	acts, err := in.Client.Activities(ctx, in.Molecule, in.Target, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, a := range acts {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resolver: URI driver string functions (network-free)
// ---------------------------------------------------------------------------

// Classify turns any accepted input into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(strings.ToUpper(input), "CHEMBL") {
		// Heuristic: targets usually have names like CHEMBL2035 and molecules
		// like CHEMBL25. We can't know without a network call, so default to
		// molecule.
		return "molecule", strings.ToUpper(input), nil
	}
	return "", "", errs.Usage("unrecognized ChEMBL reference: %q", input)
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "molecule":
		return fmt.Sprintf("https://%s/chembl/compound_report_card/%s/", Host, id), nil
	case "target":
		return fmt.Sprintf("https://%s/chembl/target_report_card/%s/", Host, id), nil
	default:
		return "", errs.Usage("chembl has no resource type %q", uriType)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mapErr(err error) error {
	return err
}
