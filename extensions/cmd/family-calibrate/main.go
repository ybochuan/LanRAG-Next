// Command family-calibrate evaluates a local, manually labelled family manifest.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ybochuan/LanRAG-Next/extensions/business"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: family-calibrate MANIFEST.json")
		os.Exit(1)
	}
	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open calibration manifest")
		os.Exit(1)
	}
	const limit = 16 << 20
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	_ = file.Close()
	if err != nil || len(raw) > limit {
		fmt.Fprintln(os.Stderr, "cannot read calibration manifest")
		os.Exit(1)
	}
	var input business.FamilyCalibrationInput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
		fmt.Fprintln(os.Stderr, "invalid calibration manifest")
		os.Exit(1)
	}
	model, err := business.LoadFamilyEmbedder(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid local embedding configuration")
		os.Exit(1)
	}
	report, err := business.CalibrateFamilySuggestionsWithModel(context.Background(), input, model)
	if err != nil {
		if errors.Is(err, business.ErrUnavailable) {
			fmt.Fprintln(os.Stderr, "local embedding model unavailable or returned invalid vectors")
		} else {
			fmt.Fprintln(os.Stderr, "expected 20–50 unique labeled pairs with disjoint train and holdout groups")
		}
		os.Exit(1)
	}
	output := json.NewEncoder(os.Stdout)
	output.SetIndent("", "  ")
	if err := output.Encode(report); err != nil {
		os.Exit(1)
	}
	if !report.Accepted {
		os.Exit(2)
	}
}
