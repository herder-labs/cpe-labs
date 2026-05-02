package handlers

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	"github.com/herder-labs/cpe-labs/internal/cwmp"
	"github.com/herder-labs/cpe-labs/internal/cwmp/soap"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

// spvHandler implements SetParameterValues.
type spvHandler struct {
	tree        *paramtree.Tree
	valueChange func(path string)
}

// NewSetParameterValues returns a cwmp.Handler implementing
// SetParameterValues against tree.
//
// valueChange is called once per path whose Raw value actually changed
// AND whose stored Notification attribute is non-zero (passive or
// active). The callback runs after the batch has been applied. Pass
// nil to disable value-change tracking.
func NewSetParameterValues(tree *paramtree.Tree, valueChange func(path string)) cwmp.Handler {
	return &spvHandler{tree: tree, valueChange: valueChange}
}

func (h *spvHandler) Method() string { return "SetParameterValues" }

func (h *spvHandler) Handle(_ context.Context, req xml.TokenReader, w io.Writer) error {
	dec := xml.NewTokenDecoder(req)

	setters, _, err := decodeSPVRequest(dec)
	if err != nil {
		drainTokens(req)
		return faultInvalidArgs(fmt.Sprintf("decode SetParameterValues: %v", err))
	}
	drainTokens(req)

	// Build setters with each leaf's existing Type so wire-side
	// xsi:type hints can never override the tree's declared type.
	// Pre-flighting via tree.Get also rejects unknown paths cleanly
	// before SetBatch sees them.
	prepared, err := h.prepareSetters(setters)
	if err != nil {
		return err
	}

	results, err := h.tree.SetBatch(prepared)
	if err != nil {
		return mapSetBatchError(err)
	}

	if h.valueChange != nil {
		for _, r := range results {
			if !r.Changed {
				continue
			}
			attrs, aerr := h.tree.GetAttributes(r.Path)
			if aerr != nil {
				continue
			}
			if attrs.Notification > 0 {
				h.valueChange(r.Path)
			}
		}
	}

	return writef(w, "      <Status>0</Status>\n")
}

// spvEntry is one decoded ParameterValueStruct from the request.
type spvEntry struct {
	Name string
	Raw  string
}

// decodeSPVRequest reads a SetParameterValues body, returning the
// parsed setters and the (currently unused) ParameterKey value. The
// decoder is positioned just inside the SetParameterValues method
// element on entry.
func decodeSPVRequest(dec *xml.Decoder) ([]spvEntry, string, error) {
	var entries []spvEntry
	var paramKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, "", err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "ParameterList":
			es, err := decodeParameterValueStructs(dec, se)
			if err != nil {
				return nil, "", err
			}
			entries = es
		case "ParameterKey":
			if err := dec.DecodeElement(&paramKey, &se); err != nil {
				return nil, "", err
			}
		default:
			if err := dec.Skip(); err != nil {
				return nil, "", err
			}
		}
	}
	return entries, paramKey, nil
}

// decodeParameterValueStructs reads <ParameterValueStruct> children
// under the supplied <ParameterList> start element, returning the
// (Name, Raw) pairs in document order.
func decodeParameterValueStructs(dec *xml.Decoder, list xml.StartElement) ([]spvEntry, error) {
	var out []spvEntry
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "ParameterValueStruct" {
				if err := dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			entry, err := decodeOneParameterValueStruct(dec, t)
			if err != nil {
				return nil, err
			}
			out = append(out, entry)
		case xml.EndElement:
			if t.Name.Local == list.Name.Local {
				return out, nil
			}
		}
	}
}

// decodeOneParameterValueStruct reads <Name> and <Value> from inside
// one <ParameterValueStruct> element and returns the pair. Other
// child elements are skipped.
func decodeOneParameterValueStruct(dec *xml.Decoder, start xml.StartElement) (spvEntry, error) {
	var entry spvEntry
	for {
		tok, err := dec.Token()
		if err != nil {
			return spvEntry{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Name":
				if err := dec.DecodeElement(&entry.Name, &t); err != nil {
					return spvEntry{}, err
				}
			case "Value":
				if err := dec.DecodeElement(&entry.Raw, &t); err != nil {
					return spvEntry{}, err
				}
			default:
				if err := dec.Skip(); err != nil {
					return spvEntry{}, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return entry, nil
			}
		}
	}
}

// prepareSetters converts decoded entries into paramtree.Setters,
// reading each existing leaf to copy its declared Type and Writable
// flag forward. Unknown paths are reported as 9005 here so the
// SetBatch pre-flight does not have to distinguish "wire-side type
// hint missing" from "path not found".
func (h *spvHandler) prepareSetters(entries []spvEntry) ([]paramtree.Setter, error) {
	out := make([]paramtree.Setter, 0, len(entries))
	for _, e := range entries {
		cur, err := h.tree.Get(e.Name)
		if err != nil {
			return nil, faultInvalidParameterName(e.Name)
		}
		out = append(out, paramtree.Setter{
			Path: e.Name,
			Value: paramtree.Value{
				Type:     cur.Type,
				Raw:      e.Raw,
				Writable: cur.Writable,
			},
		})
	}
	return out, nil
}

// mapSetBatchError converts a *paramtree.SetBatchError into the right
// CWMP *cwmp.FaultError. Any other error type maps to fault 9002.
func mapSetBatchError(err error) error {
	var sbe *paramtree.SetBatchError
	if !errors.As(err, &sbe) {
		return &cwmp.FaultError{Fault: soap.Fault{
			FaultCode:   9002,
			FaultString: err.Error(),
		}}
	}
	switch sbe.Code {
	case paramtree.FailureNotFound:
		return faultInvalidParameterName(sbe.Path)
	case paramtree.FailureNotWritable:
		return &cwmp.FaultError{Fault: soap.Fault{
			FaultCode:   9008,
			FaultString: fmt.Sprintf("Attempt to set a non-writable parameter: %s", sbe.Path),
		}}
	case paramtree.FailureTypeMismatch:
		return &cwmp.FaultError{Fault: soap.Fault{
			FaultCode:   9007,
			FaultString: fmt.Sprintf("Invalid parameter type: %s", sbe.Path),
		}}
	case paramtree.FailureInvalidValue:
		return &cwmp.FaultError{Fault: soap.Fault{
			FaultCode:   9007,
			FaultString: fmt.Sprintf("Invalid parameter value: %s: %v", sbe.Path, sbe.Err),
		}}
	case paramtree.FailureDuplicatePath:
		return faultInvalidArgs(fmt.Sprintf("duplicate path %q", sbe.Path))
	}
	return &cwmp.FaultError{Fault: soap.Fault{
		FaultCode:   9002,
		FaultString: err.Error(),
	}}
}
