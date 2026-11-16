package main

import "testing"

func TestHundredEnglishDescriptions(t *testing.T) {
	rows, err := records()
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	for _, row := range rows {
		groups[row.Category]++
	}
	if len(groups) != 10 {
		t.Fatal(groups)
	}
	for group, count := range groups {
		if count != 10 {
			t.Fatalf("%s: %d", group, count)
		}
	}
}
