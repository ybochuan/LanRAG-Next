// Command parser-calibrate measures real PDF routing against held-out labels.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ybochuan/LanRAG-Next/extensions/parser"
)

func run() int {
	manifest := flag.String("manifest", "testdata/parser/manifest.json", "version 1 labeled manifest")
	root := flag.String("root", "testdata/parser", "PDF corpus root")
	flag.Parse()
	f, err := os.Open(*manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open calibration manifest")
		return 2
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 16<<20))
	decoder.DisallowUnknownFields()
	var m parser.CalibrationManifest
	if decoder.Decode(&m) != nil || decoder.Decode(new(any)) != io.EOF {
		fmt.Fprintln(os.Stderr, "invalid calibration manifest")
		return 2
	}
	report, err := parser.Calibrate(context.Background(), *root, m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if json.NewEncoder(os.Stdout).Encode(report) != nil {
		return 2
	}
	if !report.Accepted {
		return 1
	}
	return 0
}
func main() { os.Exit(run()) }
