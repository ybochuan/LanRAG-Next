package business

import (
	"fmt"
	"testing"
)

func TestCalibrationUsesHeldOutGroupsAndRealFeatureExtraction(t *testing.T) {
	input := FamilyCalibrationInput{KnowledgeBaseID: "fixture"}
	for i := range 24 {
		positive := i%2 == 0
		expected := "none"
		peerTitle := "厨房设备采购细则"
		if positive {
			expected = "membership"
			peerTitle = "员工手册2025年修订版"
		}
		partition := "train"
		if i >= 16 {
			partition = "holdout"
		}
		input.Pairs = append(input.Pairs, FamilyCalibrationPair{
			Group: fmt.Sprintf("case-%02d", i), Partition: partition, Expected: expected,
			Peer:   FamilyCalibrationDocument{ID: fmt.Sprintf("peer-%02d", i), Title: peerTitle},
			Anchor: FamilyCalibrationDocument{ID: fmt.Sprintf("anchor-%02d", i), Title: "员工手册2026年修订版"},
		})
	}
	report, err := CalibrateFamilySuggestions(input)
	if err != nil || !report.Accepted || report.Holdout.Precision != 1 || report.Holdout.Recall != 1 || report.Holdout.Samples != 8 {
		t.Fatal(report, err)
	}
	// A failing holdout cannot influence the selected policy.
	input.Pairs[16].Expected = "none"
	input.Pairs[18].Expected = "none"
	input.Pairs[17].Expected = "membership"
	after, err := CalibrateFamilySuggestions(input)
	if err != nil || after.Accepted || after.Policy != report.Policy {
		t.Fatal(after, err)
	}
	input.Pairs[16].Group = input.Pairs[0].Group
	if _, err := CalibrateFamilySuggestions(input); err == nil {
		t.Fatal("calibration accepted a group in both partitions")
	}
}
