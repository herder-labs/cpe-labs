package clients

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

// Fabricator drives row-count churn on one multi-instance table.
type Fabricator struct {
	path        string
	typeName    string
	target      int
	rate        int
	rowDefaults map[string]string
	tree        *paramtree.Tree
	logger      *slog.Logger
}

// Options builds a Fabricator. RowDefaults values may contain
// {seq} / {seq:NN} / {seq:hex:NN} / {seq:HEX:NN} placeholders;
// fleet placeholders ({cpe}, {cpe:MAC:N}, etc.) must already be
// resolved by the caller before passing the map in.
type Options struct {
	Path        string
	Type        string
	Target      int
	ChurnRate   int
	RowDefaults map[string]string
	Tree        *paramtree.Tree
	Logger      *slog.Logger
}

func New(opts Options) (*Fabricator, error) {
	if opts.Tree == nil {
		return nil, errors.New("clients.New: Tree is nil")
	}
	if opts.Path == "" {
		return nil, errors.New("clients.New: Path is empty")
	}
	if opts.Target < 0 {
		return nil, fmt.Errorf("clients.New: Target must be >= 0, got %d", opts.Target)
	}
	if opts.ChurnRate <= 0 {
		return nil, fmt.Errorf("clients.New: ChurnRate must be > 0, got %d", opts.ChurnRate)
	}
	if !opts.Tree.IsAddDeletable(opts.Path) {
		return nil, fmt.Errorf("clients.New: path %q is not a registered multi-instance table", opts.Path)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	defaults := make(map[string]string, len(opts.RowDefaults))
	for k, v := range opts.RowDefaults {
		defaults[k] = v
	}
	return &Fabricator{
		path:        opts.Path,
		typeName:    opts.Type,
		target:      opts.Target,
		rate:        opts.ChurnRate,
		rowDefaults: defaults,
		tree:        opts.Tree,
		logger:      logger,
	}, nil
}

func (f *Fabricator) Path() string { return f.path }

// Tick reads the current row count under the table path and adjusts
// toward Target by up to ChurnRate rows. v0 is quiet at steady state
// (delta == 0 → no-op).
func (f *Fabricator) Tick(_ context.Context) error {
	instances, err := f.currentInstances()
	if err != nil {
		return err
	}
	delta := f.target - len(instances)
	switch {
	case delta > 0:
		add := delta
		if add > f.rate {
			add = f.rate
		}
		for i := 0; i < add; i++ {
			if err := f.addOne(); err != nil {
				f.logger.Warn("clients fabricator add failed", "path", f.path, "err", err.Error())
				return err
			}
		}
	case delta < 0:
		drop := -delta
		if drop > f.rate {
			drop = f.rate
		}
		// Sort instances descending so we drop the highest-numbered
		// first (LIFO matches natural lease churn).
		sort.Sort(sort.Reverse(sort.IntSlice(instances)))
		for i := 0; i < drop && i < len(instances); i++ {
			inst := instances[i]
			if err := f.dropOne(inst); err != nil {
				f.logger.Warn("clients fabricator drop failed", "path", f.path, "instance", inst, "err", err.Error())
				return err
			}
		}
	}
	return nil
}

func (f *Fabricator) currentInstances() ([]int, error) {
	children, err := f.tree.Children(f.path)
	if err != nil {
		return nil, fmt.Errorf("clients fabricator current count: %w", err)
	}
	out := make([]int, 0, len(children))
	for _, c := range children {
		if !strings.HasSuffix(c.Name, ".") {
			continue
		}
		seg := strings.TrimSuffix(strings.TrimPrefix(c.Name, f.path+"."), ".")
		n, perr := strconv.Atoi(seg)
		if perr != nil || n <= 0 {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (f *Fabricator) addOne() error {
	inst, err := f.tree.AddObject(f.path)
	if err != nil {
		return fmt.Errorf("AddObject: %w", err)
	}
	if len(f.rowDefaults) == 0 {
		f.logger.Debug("clients fabricator added row (no rowDefaults)", "path", f.path, "instance", inst)
		return nil
	}
	prefix := f.path + "." + strconv.Itoa(inst) + "."
	setters := make([]paramtree.Setter, 0, len(f.rowDefaults))
	for k, tmpl := range f.rowDefaults {
		leafPath := prefix + k
		v, gerr := f.tree.Get(leafPath)
		if gerr != nil {
			f.logger.Warn("clients fabricator: rowDefaults references unknown leaf", "path", leafPath, "err", gerr.Error())
			continue
		}
		setters = append(setters, paramtree.Setter{
			Path:  leafPath,
			Value: paramtree.Value{Type: v.Type, Raw: renderSeq(tmpl, inst), Writable: v.Writable},
		})
	}
	if len(setters) == 0 {
		return nil
	}
	if _, sErr := f.tree.SetBatch(setters); sErr != nil {
		// SetBatch failure rolls back the just-added instance.
		_ = f.tree.DeleteObject(strings.TrimSuffix(prefix, "."))
		return fmt.Errorf("SetBatch on row %d: %w", inst, sErr)
	}
	f.logger.Debug("clients fabricator added row", "path", f.path, "instance", inst)
	return nil
}

func (f *Fabricator) dropOne(instance int) error {
	path := f.path + "." + strconv.Itoa(instance)
	if err := f.tree.DeleteObject(path); err != nil {
		return fmt.Errorf("DeleteObject %s: %w", path, err)
	}
	f.logger.Debug("clients fabricator dropped row", "path", f.path, "instance", instance)
	return nil
}

// renderSeq expands {seq}, {seq:NN}, {seq:hex:NN}, {seq:HEX:NN} in s.
// Returns s unchanged if no recognized placeholder is present.
func renderSeq(s string, seq int) string {
	if !strings.Contains(s, "{seq") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if strings.HasPrefix(s[i:], "{seq") {
			end := strings.Index(s[i:], "}")
			if end < 0 {
				out.WriteString(s[i:])
				break
			}
			tok := s[i : i+end+1]
			out.WriteString(expandSeqToken(tok, seq))
			i += end + 1
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func expandSeqToken(tok string, seq int) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(tok, "{"), "}")
	switch inner {
	case "seq":
		return strconv.Itoa(seq)
	}
	// {seq:NN} or {seq:hex:NN} / {seq:HEX:NN}
	parts := strings.Split(inner, ":")
	if len(parts) < 2 || parts[0] != "seq" {
		return tok
	}
	switch len(parts) {
	case 2:
		// {seq:NN} — decimal zero-pad to NN digits
		width, err := strconv.Atoi(parts[1])
		if err != nil || width <= 0 {
			return tok
		}
		return fmt.Sprintf("%0*d", width, seq)
	case 3:
		// {seq:hex:NN} or {seq:HEX:NN}
		width, err := strconv.Atoi(parts[2])
		if err != nil || width <= 0 {
			return tok
		}
		switch parts[1] {
		case "hex":
			return fmt.Sprintf("%0*x", width, seq)
		case "HEX":
			return fmt.Sprintf("%0*X", width, seq)
		default:
			return tok
		}
	default:
		return tok
	}
}
